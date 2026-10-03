package module

/*
Verificação on-chain de um novo header QBFT (MsgUpdateClient).

O algoritmo é o mesmo do Prover off-chain (prover.go): decodifica o extraData
(RLP), recupera quem assinou via ecrecover e exige mais de 2/3 dos
validadores.

Diferenças em relação ao prover.go:
  - o encoding é sempre QBFT (5 campos no extraData, seals zerados no hash
    assinado). O Prover escolhe QBFT ou IBFT2 pela config, mas essa
    informação não existe no ClientState on-chain. As chains Besu do projeto
    rodam QBFT (bcs/besu/scripts/init_chain.sh).
  - só se confere que 2/3 do validator set do próprio header assinaram. Não
    há checagem de continuidade contra o validator set do ConsensusState
    anterior: o QBFT já exige 2/3 do set vigente a cada bloco e troca um
    validador por vez.
*/

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

/*
verifyQBFTOrderedSeals confere que mais de 2/3 dos validadores do header
assinaram headerHash.

As assinaturas não vêm de extra.Seals: esse campo está zerado, porque
BesuHeaderRlp é justamente o header sem seals que foi assinado. Elas vêm de
Header.Seals, que o Prover (validateAndGetOrderedSeals, prover.go) já entrega
alinhado por posição com extra.Validators, com nil onde o validador não
assinou. Por isso a verificação é posicional.
*/
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
