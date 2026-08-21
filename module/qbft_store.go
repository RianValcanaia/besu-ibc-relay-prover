package module

// NOTA (patch local, ver YUI_Relayer/.claude/nextsteps.md T13): este arquivo
// não existia na v0.2.8 original (que só tinha o Prover off-chain). É a
// metade on-chain que faltava - persistência de ClientState/ConsensusState
// no clientStore, portada do mesmo padrão usado por
// modules/light-clients/07-tendermint/store.go do próprio ibc-go v8 (mesma
// dependência já vendorizada), só sem a parte de iteração/pruning (não
// necessária, já que misbehaviour ficou fora do escopo do MVP - T13.5).

import (
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	host "github.com/cosmos/ibc-go/v8/modules/core/24-host"
	"github.com/cosmos/ibc-go/v8/modules/core/exported"
)

var (
	keyProcessedTime   = []byte("/processedTime")
	keyProcessedHeight = []byte("/processedHeight")
)

func setClientState(clientStore storetypes.KVStore, cdc codec.BinaryCodec, clientState *ClientState) {
	clientStore.Set(host.ClientStateKey(), clienttypes.MustMarshalClientState(cdc, clientState))
}

func getConsensusState(clientStore storetypes.KVStore, cdc codec.BinaryCodec, height exported.Height) (*ConsensusState, bool) {
	bz := clientStore.Get(host.ConsensusStateKey(height))
	if len(bz) == 0 {
		return nil, false
	}
	consensusStateI := clienttypes.MustUnmarshalConsensusState(cdc, bz)
	consensusState, ok := consensusStateI.(*ConsensusState)
	if !ok {
		return nil, false
	}
	return consensusState, true
}

func setConsensusState(clientStore storetypes.KVStore, cdc codec.BinaryCodec, consensusState *ConsensusState, height exported.Height) {
	clientStore.Set(host.ConsensusStateKey(height), clienttypes.MustMarshalConsensusState(cdc, consensusState))
}

func processedTimeKey(height exported.Height) []byte {
	return append(host.ConsensusStateKey(height), keyProcessedTime...)
}

func processedHeightKey(height exported.Height) []byte {
	return append(host.ConsensusStateKey(height), keyProcessedHeight...)
}

func setProcessedTime(clientStore storetypes.KVStore, height exported.Height, timeNs uint64) {
	clientStore.Set(processedTimeKey(height), sdk.Uint64ToBigEndian(timeNs))
}

func getProcessedTime(clientStore storetypes.KVStore, height exported.Height) (uint64, bool) {
	bz := clientStore.Get(processedTimeKey(height))
	if len(bz) == 0 {
		return 0, false
	}
	return sdk.BigEndianToUint64(bz), true
}

func setProcessedHeight(clientStore storetypes.KVStore, consHeight, processedHeight exported.Height) {
	clientStore.Set(processedHeightKey(consHeight), []byte(processedHeight.String()))
}

func getProcessedHeight(clientStore storetypes.KVStore, height exported.Height) (exported.Height, bool) {
	bz := clientStore.Get(processedHeightKey(height))
	if len(bz) == 0 {
		return nil, false
	}
	h, err := clienttypes.ParseHeight(string(bz))
	if err != nil {
		return nil, false
	}
	return h, true
}

// setConsensusMetadata records the height/time at which the executing chain processed a given
// consensus state - needed by verifyDelayPeriodPassed to enforce connection delay periods.
func setConsensusMetadata(ctx sdk.Context, clientStore storetypes.KVStore, height exported.Height) {
	setProcessedTime(clientStore, height, uint64(ctx.BlockTime().UnixNano()))
	setProcessedHeight(clientStore, height, clienttypes.GetSelfHeight(ctx))
}

func verifyDelayPeriodPassed(ctx sdk.Context, clientStore storetypes.KVStore, proofHeight exported.Height, delayTimePeriod, delayBlockPeriod uint64) error {
	if delayTimePeriod != 0 {
		processedTime, ok := getProcessedTime(clientStore, proofHeight)
		if !ok {
			return ErrProcessedTimeNotFound
		}
		currentTimestamp := uint64(ctx.BlockTime().UnixNano())
		if validTime := processedTime + delayTimePeriod; currentTimestamp < validTime {
			return ErrDelayPeriodNotPassed
		}
	}
	if delayBlockPeriod != 0 {
		processedHeight, ok := getProcessedHeight(clientStore, proofHeight)
		if !ok {
			return ErrProcessedHeightNotFound
		}
		currentHeight := clienttypes.GetSelfHeight(ctx)
		validHeight := clienttypes.NewHeight(processedHeight.GetRevisionNumber(), processedHeight.GetRevisionHeight()+delayBlockPeriod)
		if currentHeight.LT(validHeight) {
			return ErrDelayPeriodNotPassed
		}
	}
	return nil
}
