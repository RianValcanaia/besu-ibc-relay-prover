package module

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/ibc-go/v8/modules/core/exported"
	"github.com/hyperledger-labs/yui-relayer/config"
	"github.com/hyperledger-labs/yui-relayer/core"
	"github.com/spf13/cobra"
)

type Module struct{}

var _ config.ModuleI = (*Module)(nil)

// Name returns the name of the module
func (Module) Name() string {
	return "ibft2-prover"
}

// RegisterInterfaces register the module interfaces to protobuf Any.
//
// NOTA (patch local, T16, ver YUI_Relayer/.claude/nextsteps.md): faltava
// registrar Header como exported.ClientMessage - a v0.2.8 original só
// registrava ClientState/ConsensusState, o que bastava pra CreateClient
// (client hb-qbft criado com sucesso), mas quebra de verdade assim que o
// relayer tenta um MsgUpdateClient (ex.: dentro do handshake de connection,
// que atualiza o client antes de ConnOpenInit) - o nó cosmos rejeita a tx
// com "unable to resolve type URL /ibc.lightclients.qbft.v1.Header",
// confirmado rodando um handshake real contra besu_chain_0/cosmos_chain_0.
func (Module) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*core.ProverConfig)(nil),
		&ProverConfig{},
	)
	registry.RegisterImplementations(
		(*exported.ClientState)(nil),
		&ClientState{},
	)
	registry.RegisterImplementations(
		(*exported.ConsensusState)(nil),
		&ConsensusState{},
	)
	registry.RegisterImplementations(
		(*exported.ClientMessage)(nil),
		&Header{},
	)
}

// GetCmd returns the command
func (Module) GetCmd(ctx *config.Context) *cobra.Command {
	return nil
}
