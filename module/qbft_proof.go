package module

// NOTA (patch local, ver YUI_Relayer/.claude/nextsteps.md T13.3): verificação
// de membership/non-membership contra o storage_root do IBCHandler, via
// prova MPT (eth_getProof). Não é criptografia nova - go-ethereum já resolve
// (trie.VerifyProof); o trabalho aqui é só reverter o encoding que
// ethereum-ibc-relay-chain/pkg/client/proof.go usa pra empacotar a prova
// (rlp.EncodeToBytes de uma lista de nós já decodificados, ver proof.go
// daquele repo) de volta pros bytes RLP originais de cada nó da trie, e
// então montar o storage key do jeito que buildStateProof (prover.go, já
// vendorizado) já calcula: keccak256(keccak256(path) || IBCCommitmentsSlot).
//
// Limitação documentada (MVP): o valor lido da trie é comparado com o
// `value` esperado após padding à esquerda pra 32 bytes - assume-se que os
// commitments do IBCHandler.sol são sempre bytes32 (é o caso de todos os
// commitments ICS-24 gerados pelo ibc-go: hash sha256 de 32 bytes).

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

// decodeMPTProofNodes reverte o encoding usado por
// ethereum-ibc-relay-chain/pkg/client/proof.go (encodeRLP): a prova chega
// como um blob RLP de uma lista de nós já decodificados (cada nó como
// [][]byte); aqui recuperamos os bytes RLP originais de cada nó
// re-codificando cada item da lista.
func decodeMPTProofNodes(proofRLP []byte) ([][]byte, error) {
	var decoded [][][]byte
	if err := rlp.DecodeBytes(proofRLP, &decoded); err != nil {
		return nil, fmt.Errorf("failed to decode mpt proof: %w", err)
	}
	nodes := make([][]byte, 0, len(decoded))
	for _, item := range decoded {
		b, err := rlp.EncodeToBytes(item)
		if err != nil {
			return nil, fmt.Errorf("failed to re-encode mpt proof node: %w", err)
		}
		nodes = append(nodes, b)
	}
	return nodes, nil
}

// buildProofDB monta um key-value store em memória (nó indexado pelo seu
// próprio hash) do jeito que trie.VerifyProof espera receber a prova.
func buildProofDB(nodes [][]byte) *memorydb.Database {
	db := memorydb.New()
	for _, n := range nodes {
		_ = db.Put(crypto.Keccak256(n), n)
	}
	return db
}

// storageTrieKey calcula o slot de storage do commitment ICS-24 (mesma
// fórmula usada por Prover.buildStateProof em prover.go) e o transforma na
// chave real da secure trie (keccak256 do slot).
func storageTrieKey(path []byte) ([]byte, error) {
	storageKey := crypto.Keccak256Hash(append(
		crypto.Keccak256Hash(path).Bytes(),
		IBCCommitmentsSlot.Bytes()...,
	))
	return crypto.Keccak256(storageKey.Bytes()), nil
}

// verifyMPTMembership confere se a prova (formato ethereum-ibc-relay-chain)
// demonstra a presença (ou ausência) de `path` no storage_root informado, e
// devolve o valor bruto (RLP-decodificado) encontrado, se houver.
func verifyMPTMembership(storageRoot []byte, path []byte, proofRLP []byte) (found bool, value []byte, err error) {
	nodes, err := decodeMPTProofNodes(proofRLP)
	if err != nil {
		return false, nil, err
	}
	key, err := storageTrieKey(path)
	if err != nil {
		return false, nil, err
	}
	db := buildProofDB(nodes)
	raw, err := trie.VerifyProof(common.BytesToHash(storageRoot), key, db)
	if err != nil {
		return false, nil, fmt.Errorf("invalid mpt proof: %w", err)
	}
	if raw == nil {
		return false, nil, nil
	}
	var decoded []byte
	if err := rlp.DecodeBytes(raw, &decoded); err != nil {
		return false, nil, fmt.Errorf("failed to decode storage value: %w", err)
	}
	return true, decoded, nil
}

// pad32 preenche à esquerda com zeros até 32 bytes (valores da storage trie
// vêm com zeros à esquerda suprimidos - ver limitação documentada acima).
func pad32(b []byte) []byte {
	if len(b) >= 32 {
		return b[len(b)-32:]
	}
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}

// gethAccount é o layout RLP de uma conta Ethereum (nonce, balance, storage
// root, code hash) - mesmo formato que github.com/ethereum/go-ethereum/core/types.StateAccount
// usa internamente, mas evitamos importar esse pacote (não exportado da
// forma que precisamos) e decodificamos direto.
type gethAccount struct {
	Nonce    uint64
	Balance  *big.Int
	Root     common.Hash
	CodeHash []byte
}

// verifyAccountStorageRoot confere, contra o stateRoot do header (já
// autenticado pelas assinaturas QBFT em qbft_update.go), que a conta do
// IBCHandler tem o storage_root informado - é essa autenticação que dá
// legitimidade ao novo ConsensusState.Root persistido em UpdateState.
func verifyAccountStorageRoot(header *types.Header, accountProofNodes [][]byte, ibcStoreAddress []byte) ([]byte, error) {
	db := buildProofDB(accountProofNodes)
	key := crypto.Keccak256(ibcStoreAddress)
	raw, err := trie.VerifyProof(header.Root, key, db)
	if err != nil {
		return nil, fmt.Errorf("invalid account proof: %w", err)
	}
	if raw == nil {
		return nil, fmt.Errorf("ibc store account not found in state trie (address=%x)", ibcStoreAddress)
	}
	var acct gethAccount
	if err := rlp.DecodeBytes(raw, &acct); err != nil {
		return nil, fmt.Errorf("failed to decode account: %w", err)
	}
	return acct.Root.Bytes(), nil
}
