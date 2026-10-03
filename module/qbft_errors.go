package module

/*
Erros sentinela da parte on-chain do light client hb-qbft (qbft_store.go,
qbft_proof.go, qbft_update.go, qbft.go), usados na checagem do delay period
da connection.
*/

import "errors"

var (
	ErrProcessedTimeNotFound   = errors.New("qbft: processed time not found for height")
	ErrProcessedHeightNotFound = errors.New("qbft: processed height not found for height")
	ErrDelayPeriodNotPassed    = errors.New("qbft: connection delay period has not yet passed")
)
