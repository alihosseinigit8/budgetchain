// This file defines the shared block and transaction model for all four
// ledgers. The transaction type identifies its architectural workflow stage.
package types

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"time"
)

type TxType string

const (
	TxIdentityRegistration TxType = "identity_registration"
	TxPolicyUpdate         TxType = "policy_update"
	TxBudgetRequest        TxType = "budget_request"
	TxBudgetApproval       TxType = "budget_approval"
	TxBudgetDisclosure     TxType = "budget_disclosure"
	TxTaxAuthority         TxType = "tax_authority_registration"
	TxTaxAttestation       TxType = "tax_attestation"
	TxTaxDisclosure        TxType = "tax_disclosure"
)

// Tx is an event recorded in a block. Signatures are part of the transaction
// hash, so request and organizational approval signatures are auditable through
// the TxRoot and, ultimately, the block hash. Public-ledger transactions have
// no signatures because they contain only sanitized data.
type Tx struct {
	Type       TxType      `json:"type"`
	Sender     string      `json:"sender"`
	Payload    []byte      `json:"payload"`
	Signatures []Signature `json:"signatures,omitempty"`
}

func (tx Tx) Hash() []byte {
	h := sha256.New()
	h.Write([]byte(tx.Type))
	h.Write([]byte(tx.Sender))
	h.Write(tx.Payload)
	for _, signature := range tx.Signatures {
		h.Write([]byte(signature.SignerID))
		h.Write([]byte(signature.Value))
	}
	return h.Sum(nil)
}

// Header contains the minimum data linking a block to its predecessor and to
// state after transaction execution. Budget and tax ledgers have different roots.
type Header struct {
	Height    uint64 `json:"height"`
	PrevHash  []byte `json:"prev_hash"`
	TxRoot    []byte `json:"tx_root"`
	StateRoot []byte `json:"state_root"`
	TimeUnix  int64  `json:"time_unix"`
}

// Block is the append-only unit of each ledger. BudgetPrivate transactions may
// contain signatures and details, while BudgetPublic transactions contain only
// minimal records.
type Block struct {
	Header Header `json:"header"`
	Txs    []Tx   `json:"txs"`
	Hash   []byte `json:"hash"`
}

// TxRoot combines every transaction hash in a block into one value so changes to
// a signature or payload can be detected during block validation.
func TxRoot(txs []Tx) []byte {
	h := sha256.New()
	for _, tx := range txs {
		h.Write(tx.Hash())
	}
	return h.Sum(nil)
}

// ComputeBlockHash deterministically hashes the header, protecting the prev_hash,
// TxRoot, and StateRoot link together.
func ComputeBlockHash(hdr Header) []byte {
	h := sha256.New()
	_ = binary.Write(h, binary.BigEndian, hdr.Height)
	h.Write(hdr.PrevHash)
	h.Write(hdr.TxRoot)
	h.Write(hdr.StateRoot)
	_ = binary.Write(h, binary.BigEndian, hdr.TimeUnix)
	return h.Sum(nil)
}

// NewBlock creates a non-genesis block that references the prior tip hash.
func NewBlock(height uint64, prev, stateRoot []byte, txs []Tx) *Block {
	hdr := Header{
		Height:    height,
		PrevHash:  prev,
		TxRoot:    TxRoot(txs),
		StateRoot: stateRoot,
		TimeUnix:  time.Now().UTC().Unix(),
	}
	return &Block{Header: hdr, Txs: txs, Hash: ComputeBlockHash(hdr)}
}

// GenesisBlock creates the independent first block for one of the four ledgers.
func GenesisBlock(stateRoot []byte) *Block {
	hdr := Header{
		Height:    0,
		PrevHash:  make([]byte, 32),
		TxRoot:    TxRoot(nil),
		StateRoot: stateRoot,
		TimeUnix:  time.Now().UTC().Unix(),
	}
	return &Block{Header: hdr, Hash: ComputeBlockHash(hdr)}
}

func (b *Block) HashHex() string     { return hex.EncodeToString(b.Hash) }
func (b *Block) PrevHashHex() string { return hex.EncodeToString(b.Header.PrevHash) }

// MustJSON serializes a structured payload for a Tx. Failure is unrecoverable in
// this PoC because every internal type is expected to be JSON-serializable.
func MustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// JSONDigest is used for in-memory StateRoots. It does not replace organizational
// signatures; it only provides a state commitment.
func JSONDigest(v any) []byte {
	sum := sha256.Sum256(MustJSON(v))
	return sum[:]
}
