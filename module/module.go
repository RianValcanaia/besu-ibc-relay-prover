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

/*
RegisterInterfaces registra os tipos do módulo no InterfaceRegistry, para que
possam ser empacotados em Any.

Além de ProverConfig, ClientState e ConsensusState, registra o Header como
exported.ClientMessage. Sem isso o MsgUpdateClient é rejeitado com "unable
to resolve type URL /ibc.lightclients.qbft.v1.Header" e o handshake quebra
na primeira atualização do client.
*/
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
