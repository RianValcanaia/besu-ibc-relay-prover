package module

// NOTA (patch local, T13): erros sentinela usados pela metade on-chain do
// light client (qbft_store.go, qbft_proof.go, qbft_update.go, qbft.go).

import "errors"

var (
	ErrProcessedTimeNotFound   = errors.New("qbft: processed time not found for height")
	ErrProcessedHeightNotFound = errors.New("qbft: processed height not found for height")
	ErrDelayPeriodNotPassed    = errors.New("qbft: connection delay period has not yet passed")
)
