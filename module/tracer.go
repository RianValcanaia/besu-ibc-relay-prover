package module

/*
Tracer OpenTelemetry do pacote. ProverConfig.Build (config.go) usa ele para
envolver o Prover com otelcore.NewProver, no mesmo padrão do
ethereum-ibc-relay-chain e do chains/tendermint do yui-relayer.
*/

import "go.opentelemetry.io/otel"

var (
	tracer = otel.Tracer("github.com/datachainlab/besu-ibc-relay-prover/module")
)
