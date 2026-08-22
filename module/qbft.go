package module

// NOTA (patch local, ver YUI_Relayer/.claude/nextsteps.md T13): a v0.2.8
// original só implementava a metade off-chain (Prover, em prover.go) - os
// métodos de exported.ClientState/exported.ConsensusState aqui eram todos
// panic("not implemented"). Este arquivo (+ qbft_store.go, qbft_proof.go,
// qbft_update.go, qbft_errors.go, todos adicionados neste patch local)
// implementa a metade on-chain de verdade, reaproveitando o mesmo tipo
// hb-qbft e a mesma lógica de verificação (RLP/ecrecover/threshold) que o
// Prover já usava off-chain, em vez de desenhar um client novo do zero -
// ver YUI_Relayer/.claude/claude.md, "antes de implementar, procure se já
// não existe pronto".
//
// Fora do MVP (documentado, mesma decisão já registrada em besu.md §5.6.1):
// misbehaviour/equivocation (CheckForMisbehaviour sempre retorna false),
// client recovery (CheckSubstituteAndUpdateState) e chain upgrades
// (VerifyUpgradeAndUpdateState) - nenhum dos três é exercitado pelo fluxo
// de handshake + transferência ICS-20 que é o objetivo desta fase (T15-T17).

import (
	"fmt"
	"time"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	commitmenttypes "github.com/cosmos/ibc-go/v8/modules/core/23-commitment/types"
	"github.com/cosmos/ibc-go/v8/modules/core/exported"
	"github.com/ethereum/go-ethereum/crypto"
)

const QBFT_CLIENT_TYPE = "hb-qbft"

var _ exported.ClientState = (*ClientState)(nil)

func (cs *ClientState) ClientType() string {
	return QBFT_CLIENT_TYPE
}

func (cs *ClientState) GetLatestHeight() exported.Height {
	return cs.LatestHeight
}

func (cs *ClientState) Validate() error {
	if len(cs.IbcStoreAddress) != 20 {
		return fmt.Errorf("invalid ibc store address length: got %d, want 20", len(cs.IbcStoreAddress))
	}
	if cs.LatestHeight.IsZero() {
		return fmt.Errorf("latest height cannot be zero")
	}
	return nil
}

// Status considera o client Active enquanto o ConsensusState mais recente
// não tiver expirado (trusting period == 0 desativa a checagem, mesma
// convenção já documentada no proto). Não há noção de Frozen (misbehaviour
// fora do MVP).
func (cs *ClientState) Status(ctx sdk.Context, clientStore storetypes.KVStore, cdc codec.BinaryCodec) exported.Status {
	consState, found := getConsensusState(clientStore, cdc, cs.LatestHeight)
	if !found {
		return exported.Expired
	}
	if cs.TrustingPeriod != 0 {
		expirationTime := time.Unix(int64(consState.Timestamp), 0).Add(time.Duration(cs.TrustingPeriod) * time.Second)
		if !expirationTime.After(ctx.BlockTime()) {
			return exported.Expired
		}
	}
	return exported.Active
}

// ExportMetadata: sem metadata adicional pra exportar no MVP (genesis
// export/import não faz parte do fluxo T15-T17).
func (cs *ClientState) ExportMetadata(clientStore storetypes.KVStore) []exported.GenesisMetadata {
	return nil
}

func (cs *ClientState) ZeroCustomFields() exported.ClientState {
	return &ClientState{
		ChainId:         cs.ChainId,
		IbcStoreAddress: cs.IbcStoreAddress,
		LatestHeight:    cs.LatestHeight,
	}
}

func (cs *ClientState) GetTimestampAtHeight(ctx sdk.Context, clientStore storetypes.KVStore, cdc codec.BinaryCodec, height exported.Height) (uint64, error) {
	consState, found := getConsensusState(clientStore, cdc, height)
	if !found {
		return 0, fmt.Errorf("consensus state not found at height %s", height)
	}
	return consState.GetTimestamp(), nil
}

func (cs *ClientState) Initialize(ctx sdk.Context, cdc codec.BinaryCodec, clientStore storetypes.KVStore, consensusState exported.ConsensusState) error {
	consState, ok := consensusState.(*ConsensusState)
	if !ok {
		return fmt.Errorf("invalid initial consensus state. expected type: %T, got: %T", &ConsensusState{}, consensusState)
	}
	setClientState(clientStore, cdc, cs)
	setConsensusState(clientStore, cdc, consState, cs.LatestHeight)
	setConsensusMetadata(ctx, clientStore, cs.LatestHeight)
	return nil
}

// VerifyMembership/VerifyNonMembership verificam uma prova MPT (eth_getProof)
// contra o storage_root guardado no ConsensusState daquela altura. Ver
// qbft_proof.go pra lógica de decodificação/verificação em si.
func (cs *ClientState) VerifyMembership(ctx sdk.Context, clientStore storetypes.KVStore, cdc codec.BinaryCodec, height exported.Height, delayTimePeriod uint64, delayBlockPeriod uint64, proof []byte, path exported.Path, value []byte) error {
	if cs.GetLatestHeight().LT(height) {
		return fmt.Errorf("client state height < proof height (%s < %s)", cs.GetLatestHeight(), height)
	}
	if err := verifyDelayPeriodPassed(ctx, clientStore, height, delayTimePeriod, delayBlockPeriod); err != nil {
		return err
	}
	consState, found := getConsensusState(clientStore, cdc, height)
	if !found {
		return fmt.Errorf("consensus state not found at height %s", height)
	}
	icsPath, err := ics24Path(path)
	if err != nil {
		return err
	}
	found_, got, err := verifyMPTMembership(consState.Root, icsPath, proof)
	if err != nil {
		return err
	}
	if !found_ {
		return fmt.Errorf("membership proof failed: key not found in storage trie")
	}
	// Achado real (T22): todo commitment do IBCHandler.sol (client state,
	// consensus state, connection, channel, packet - ver
	// IBCClient.sol/IBCConnection.sol/IBCChannel*.sol, sempre
	// `commitments[key] = keccak256(value)`) guarda o HASH do valor, nunca
	// o valor cru - `value` aqui é o ConnectionEnd/ChannelEnd/etc completo
	// (várias dezenas de bytes), não os 32 bytes que a trie realmente
	// armazena. Comparar `value` cru (mesmo com pad32) contra o que a trie
	// devolve sempre falhava pra qualquer commitment que não já fosse um
	// hash de 32 bytes (ex.: ConnOpenAck rejeitando a prova de
	// ConnectionEnd de um `TendermintClient.sol` real, T20/T21) - pego
	// rodando o handshake de verdade pela primeira vez com verificação
	// real nas duas pontas.
	wantHash := crypto.Keccak256(value)
	gotPadded := pad32(got)
	wantPadded := pad32(wantHash)
	for i := range gotPadded {
		if gotPadded[i] != wantPadded[i] {
			return fmt.Errorf("membership proof failed: value mismatch (got %x, want %x)", got, wantHash)
		}
	}
	return nil
}

func (cs *ClientState) VerifyNonMembership(ctx sdk.Context, clientStore storetypes.KVStore, cdc codec.BinaryCodec, height exported.Height, delayTimePeriod uint64, delayBlockPeriod uint64, proof []byte, path exported.Path) error {
	if cs.GetLatestHeight().LT(height) {
		return fmt.Errorf("client state height < proof height (%s < %s)", cs.GetLatestHeight(), height)
	}
	if err := verifyDelayPeriodPassed(ctx, clientStore, height, delayTimePeriod, delayBlockPeriod); err != nil {
		return err
	}
	consState, found := getConsensusState(clientStore, cdc, height)
	if !found {
		return fmt.Errorf("consensus state not found at height %s", height)
	}
	icsPath, err := ics24Path(path)
	if err != nil {
		return err
	}
	found_, _, err := verifyMPTMembership(consState.Root, icsPath, proof)
	if err != nil {
		return err
	}
	if found_ {
		return fmt.Errorf("non-membership proof failed: key was found in storage trie")
	}
	return nil
}

// ics24Path extrai o path ICS-24 puro (sem o prefixo de commitment) de um
// exported.Path - mesmo componente usado por Prover.ProveState (via
// host.FullClientStatePath etc.) do lado do relayer.
func ics24Path(path exported.Path) ([]byte, error) {
	merklePath, ok := path.(commitmenttypes.MerklePath)
	if !ok {
		return nil, fmt.Errorf("expected %T, got %T", commitmenttypes.MerklePath{}, path)
	}
	if len(merklePath.KeyPath) == 0 {
		return nil, fmt.Errorf("empty merkle path")
	}
	return []byte(merklePath.KeyPath[len(merklePath.KeyPath)-1]), nil
}

func (cs *ClientState) VerifyClientMessage(ctx sdk.Context, cdc codec.BinaryCodec, clientStore storetypes.KVStore, clientMsg exported.ClientMessage) error {
	return cs.verifyClientMessage(ctx, cdc, clientStore, clientMsg)
}

// CheckForMisbehaviour: sempre false - equivocation/misbehaviour fica fora
// do MVP (ver nota do topo do arquivo).
func (cs *ClientState) CheckForMisbehaviour(ctx sdk.Context, cdc codec.BinaryCodec, clientStore storetypes.KVStore, clientMsg exported.ClientMessage) bool {
	return false
}

// UpdateStateOnMisbehaviour nunca é chamado (CheckForMisbehaviour sempre
// false) - mantido como panic explícito só pra deixar isso claro se algum
// dia deixar de ser verdade.
func (cs *ClientState) UpdateStateOnMisbehaviour(ctx sdk.Context, cdc codec.BinaryCodec, clientStore storetypes.KVStore, clientMsg exported.ClientMessage) {
	panic("qbft: misbehaviour handling not implemented (out of MVP scope, see nextsteps.md T13.5)")
}

func (cs *ClientState) UpdateState(ctx sdk.Context, cdc codec.BinaryCodec, clientStore storetypes.KVStore, clientMsg exported.ClientMessage) []exported.Height {
	return cs.updateState(ctx, cdc, clientStore, clientMsg)
}

// CheckSubstituteAndUpdateState: client recovery fora do MVP.
func (cs *ClientState) CheckSubstituteAndUpdateState(ctx sdk.Context, cdc codec.BinaryCodec, subjectClientStore, substituteClientStore storetypes.KVStore, substituteClient exported.ClientState) error {
	return fmt.Errorf("qbft: client recovery not implemented (out of MVP scope, see nextsteps.md T13)")
}

// VerifyUpgradeAndUpdateState: chain upgrades fora do MVP.
func (cs *ClientState) VerifyUpgradeAndUpdateState(ctx sdk.Context, cdc codec.BinaryCodec, store storetypes.KVStore, newClient exported.ClientState, newConsState exported.ConsensusState, proofUpgradeClient, proofUpgradeConsState []byte) error {
	return fmt.Errorf("qbft: chain upgrades not implemented (out of MVP scope, see nextsteps.md T13)")
}

var _ exported.ConsensusState = (*ConsensusState)(nil)

func (cs *ConsensusState) ClientType() string {
	return QBFT_CLIENT_TYPE
}

func (cs *ConsensusState) GetTimestamp() uint64 {
	return uint64(time.Unix(int64(cs.Timestamp), 0).UnixNano())
}

func (cs *ConsensusState) ValidateBasic() error {
	if len(cs.Root) == 0 {
		return fmt.Errorf("root cannot be empty")
	}
	if len(cs.Validators) == 0 {
		return fmt.Errorf("validators cannot be empty")
	}
	return nil
}
