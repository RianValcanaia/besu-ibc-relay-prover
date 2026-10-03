package module

import (
	"log"

	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	"github.com/cosmos/ibc-go/v8/modules/core/exported"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/hyperledger-labs/yui-relayer/core"
)

var _ core.Header = (*Header)(nil)

func (Header) ClientType() string {
	return QBFT_CLIENT_TYPE
}

func (h *Header) GetHeight() exported.Height {
	ethHeader, err := h.decodeEthHeader()
	if err != nil {
		log.Panicf("invalid header: %v", h)
	}
	return ethHeightToPB(ethHeader.Number.Uint64())
}

func (h *Header) ValidateBasic() error {
	if _, err := h.decodeEthHeader(); err != nil {
		return err
	}
	if _, err := h.decodeAccountProof(); err != nil {
		return err
	}
	return nil
}

func (h *Header) decodeEthHeader() (*types.Header, error) {
	var ethHeader types.Header
	if err := rlp.DecodeBytes(h.BesuHeaderRlp, &ethHeader); err != nil {
		return nil, err
	}
	return &ethHeader, nil
}

/*
decodeAccountProof desfaz o encoding do ethereum-ibc-relay-chain
(pkg/client/proof.go) e devolve os bytes RLP originais de cada nó da trie.
Usa o mesmo decodeMPTProofNodes da verificação on-chain (qbft_proof.go).
*/
func (h *Header) decodeAccountProof() ([][]byte, error) {
	return decodeMPTProofNodes(h.AccountStateProof)
}

func ethHeightToPB(height uint64) clienttypes.Height {
	return clienttypes.NewHeight(0, uint64(height))
}
