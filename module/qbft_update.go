package module

// NOTA (patch local, ver YUI_Relayer/.claude/nextsteps.md T13.2): a metade
// on-chain de "verificar um novo header QBFT" que faltava. O algoritmo
// (decodificar extraData RLP, recuperar assinaturas via ecrecover, conferir
// threshold >2/3) é portado 1:1 de Prover.validateAndGetOrderedSeals /
// recoverSeals / ecrecover, já existentes em prover.go neste mesmo pacote -
// reaproveitados diretamente aqui, não reescritos.
//
// Diferença deliberada em relação a prover.go: aqui fixamos sempre o
// encoding QBFT (5 campos no extraData, seals zerados pro cálculo do hash
// assinado) em vez de checar pr.config.IsIBFT2(), porque essa informação
// (qual consenso) só existe na config do Prover (off-chain), não no
// ClientState on-chain - e o besu_chain_0 deste projeto roda QBFT (ver
// bcs/besu/scripts/init_chain.sh), não IBFT2. Suporte a IBFT2 on-chain fica
// para quando/se for necessário (não é o caso hoje).
//
// Limitação documentada (MVP, mesmo espírito da decisão de deixar
// misbehaviour.go fora do MVP, já registrada em besu.md §5.6.1): só
// verificamos que ≥2/3 do validator set do PRÓPRIO header assinaram (igual
// ao Prover off-chain). Não há checagem adicional de continuidade contra o
// validator set do ConsensusState confiável anterior (ex.: sobreposição
// mínima entre o set antigo e o novo) - o QBFT já garante isso a cada bloco
// individualmente (mudanças de validador acontecem uma де cada vez, via
// Vote, e cada bloco intermediário já precisa de 2/3 do set vigente), mas um
// hardening mais forte (bridging explícito) pode ser adicionado depois se
// necessário.

import (
	"fmt"
	"time"

	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	"github.com/cosmos/ibc-go/v8/modules/core/exported"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// verifyQBFTOrderedSeals confere que mais de 2/3 dos validadores listados no
// header assinaram headerHash. IMPORTANTE (achado real, ver T13.6): as
// assinaturas não vêm de extra.Seals (o extraData decodificado de
// Header.BesuHeaderRlp) - esse campo já está zerado, porque BesuHeaderRlp é
// exatamente o header "sem seals" que foi assinado (ver
// Prover.validateAndGetOrderedSeals em prover.go). As assinaturas reais são
// o campo Header.Seals, já ordenado/alinhado posicionalmente com
// extra.Validators (com nil nas posições que não assinaram) pela mesma
// função, do lado off-chain. Por isso a verificação aqui é posicional (mais
// simples até que o recoverSeals baseado em mapa que o Prover usa
// off-chain), não por endereço->assinatura.
func verifyQBFTOrderedSeals(headerHash []byte, validators []common.Address, seals [][]byte) error {
	if len(seals) != len(validators) {
		return fmt.Errorf("seals/validators length mismatch: %d != %d", len(seals), len(validators))
	}
	count := 0
	for i, seal := range seals {
		if len(seal) == 0 {
			continue
		}
		addr, err := ecrecover(headerHash, seal)
		if err != nil {
			return fmt.Errorf("failed to recover seal at index %d: %w", i, err)
		}
		if addr != validators[i] {
			return fmt.Errorf("seal at index %d does not match validator %s (recovered %s)", i, validators[i], addr)
		}
		count++
	}
	if threshold := len(validators) * 2 / 3; count <= threshold {
		return fmt.Errorf("insufficient voting: %d <= %d (of %d validators)", count, threshold, len(validators))
	}
	return nil
}

// verifyClientMessage é a lógica real de VerifyClientMessage (chamada pelo
// method em qbft.go). Separada num arquivo próprio pra espelhar o
// update.go do 07-tendermint.
func (cs *ClientState) verifyClientMessage(ctx sdk.Context, cdc codec.BinaryCodec, clientStore storetypes.KVStore, clientMsg exported.ClientMessage) error {
	header, ok := clientMsg.(*Header)
	if !ok {
		return fmt.Errorf("invalid client message type: %T", clientMsg)
	}
	if err := header.ValidateBasic(); err != nil {
		return err
	}

	ethHeader, err := header.decodeEthHeader()
	if err != nil {
		return err
	}

	trustedConsState, found := getConsensusState(clientStore, cdc, header.TrustedHeight)
	if !found {
		return fmt.Errorf("trusted consensus state not found at height %s", header.TrustedHeight)
	}

	headerTime := time.Unix(int64(ethHeader.Time), 0)
	if cs.TrustingPeriod != 0 {
		expirationTime := time.Unix(int64(trustedConsState.Timestamp), 0).Add(time.Duration(cs.TrustingPeriod) * time.Second)
		if !expirationTime.After(ctx.BlockTime()) {
			return fmt.Errorf("trusted consensus state at height %s is expired (trusting period %ds)", header.TrustedHeight, cs.TrustingPeriod)
		}
	}
	if cs.MaxClockDrift != 0 && headerTime.After(ctx.BlockTime().Add(time.Duration(cs.MaxClockDrift)*time.Second)) {
		return fmt.Errorf("header time %s is too far in the future (max clock drift %ds)", headerTime, cs.MaxClockDrift)
	}

	if !header.GetHeight().GT(header.TrustedHeight) {
		return fmt.Errorf("header height %s must be greater than trusted height %s", header.GetHeight(), header.TrustedHeight)
	}

	extra, err := parseExtraData(ethHeader.Extra)
	if err != nil {
		return err
	}
	headerHash := crypto.Keccak256(header.BesuHeaderRlp)
	if err := verifyQBFTOrderedSeals(headerHash, extra.Validators, header.Seals); err != nil {
		return err
	}

	accountProofNodes, err := header.decodeAccountProof()
	if err != nil {
		return err
	}
	if _, err := verifyAccountStorageRoot(ethHeader, accountProofNodes, cs.IbcStoreAddress); err != nil {
		return err
	}

	return nil
}

// updateState é a lógica real de UpdateState. Assume que verifyClientMessage
// já validou o header (contrato da interface exported.ClientState).
func (cs *ClientState) updateState(ctx sdk.Context, cdc codec.BinaryCodec, clientStore storetypes.KVStore, clientMsg exported.ClientMessage) []exported.Height {
	header := clientMsg.(*Header)
	ethHeader, err := header.decodeEthHeader()
	if err != nil {
		panic(err) // já validado em verifyClientMessage
	}
	accountProofNodes, err := header.decodeAccountProof()
	if err != nil {
		panic(err)
	}
	storageRoot, err := verifyAccountStorageRoot(ethHeader, accountProofNodes, cs.IbcStoreAddress)
	if err != nil {
		panic(err)
	}
	extra, err := parseExtraData(ethHeader.Extra)
	if err != nil {
		panic(err)
	}
	var validators [][]byte
	for _, v := range extra.Validators {
		validators = append(validators, v.Bytes())
	}

	newHeight := header.GetHeight().(clienttypes.Height)
	newConsState := &ConsensusState{
		Timestamp:  ethHeader.Time,
		Root:       storageRoot,
		Validators: validators,
	}
	setConsensusState(clientStore, cdc, newConsState, newHeight)
	setConsensusMetadata(ctx, clientStore, newHeight)

	if cs.LatestHeight.LT(newHeight) {
		cs.LatestHeight = newHeight
		setClientState(clientStore, cdc, cs)
	}

	return []exported.Height{newHeight}
}
