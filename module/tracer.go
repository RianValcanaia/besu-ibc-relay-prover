package module

// NOTA (patch local, T15, ver YUI_Relayer/.claude/nextsteps.md): faltava
// nesse pacote - mesmo padrão de tracer.go em ethereum-ibc-relay-chain e
// yui-relayer/chains/tendermint, necessário pra envolver o Prover retornado
// por ProverConfig.Build com otelcore.NewProver (ver config.go).

import "go.opentelemetry.io/otel"

var (
	tracer = otel.Tracer("github.com/datachainlab/besu-ibc-relay-prover/module")
)
