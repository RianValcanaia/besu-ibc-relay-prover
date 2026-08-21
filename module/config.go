package module

import (
	"fmt"
	"time"

	"github.com/datachainlab/ethereum-ibc-relay-chain/pkg/relay/ethereum"
	"github.com/hyperledger-labs/yui-relayer/core"
	"github.com/hyperledger-labs/yui-relayer/coreutil"
	"github.com/hyperledger-labs/yui-relayer/otelcore"
)

const (
	QBFTConsensusType  = "qbft"
	IBFT2ConsensusType = "ibft2"
)

var _ core.ProverConfig = (*ProverConfig)(nil)

// Build implementa core.ProverConfig.Build.
//
// NOTA (patch local, T15, ver YUI_Relayer/.claude/nextsteps.md): a v0.2.8
// original fazia um type assertion direto (`chain.(*ethereum.Chain)`), que
// quebra de verdade quando o Prover é construído pelo fluxo padrão do CLI
// do yui-relayer (`chains add-dir`) - `ChainConfig.Build()` (tanto o de
// `ethereum-ibc-relay-chain` quanto o de `chains/tendermint`) sempre
// envolve a chain concreta num `otelcore.Chain` (tracing), então o que
// chega aqui nunca é um `*ethereum.Chain` puro. Confirmado rodando de
// verdade contra uma besu_chain_0/cosmos_chain_0 reais via `yrly chains
// add-dir`: erro real "chain type must be *ethereum.Chain, not
// *otelcore.Chain". O próprio `chains/tendermint/config.go` (que já
// funciona) resolve isso com `coreutil.UnwrapChain[*Chain](chain)` - é
// exatamente esse helper, já existente no `yui-relayer`, que faltava usar
// aqui em vez de reinventar a lógica de desembrulhar a chain.
func (c ProverConfig) Build(chain core.Chain) (core.Prover, error) {
	chain_, err := coreutil.UnwrapChain[*ethereum.Chain](chain)
	if err != nil {
		return nil, fmt.Errorf("chain type must be %T: %w", &ethereum.Chain{}, err)
	}
	return otelcore.NewProver(NewProver(chain_, c), chain.ChainID(), tracer), nil
}

func (c ProverConfig) Validate() error {
	if c.ConsensusType != "" && c.ConsensusType != QBFTConsensusType && c.ConsensusType != IBFT2ConsensusType {
		return fmt.Errorf("invalid consensus type: %s", c.ConsensusType)
	}
	if c.TrustingPeriod != "" {
		if _, err := time.ParseDuration(c.TrustingPeriod); err != nil {
			return fmt.Errorf("invalid trusting period: %s", c.TrustingPeriod)
		}
	}
	if c.MaxClockDrift != "" {
		if _, err := time.ParseDuration(c.MaxClockDrift); err != nil {
			return fmt.Errorf("invalid max clock drift: %s", c.MaxClockDrift)
		}
	}
	return nil
}

func (c ProverConfig) IsIBFT2() bool {
	return c.ConsensusType == IBFT2ConsensusType
}

func (c ProverConfig) GetTrustingPeriod() time.Duration {
	if c.TrustingPeriod == "" {
		return 0
	}
	d, err := time.ParseDuration(c.TrustingPeriod)
	if err != nil {
		panic(err)
	}
	return d
}

func (c ProverConfig) GetMaxClockDrift() time.Duration {
	if c.MaxClockDrift == "" {
		return 0
	}
	d, err := time.ParseDuration(c.MaxClockDrift)
	if err != nil {
		panic(err)
	}
	return d
}
