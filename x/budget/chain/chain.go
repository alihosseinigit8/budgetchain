// Package chain implements the generic block chain. The same structure is used
// by all four ledgers, but each has its own genesis block, StateRoot, and data;
// sharing code does not make the ledgers identical.
package chain

import (
	"bytes"
	"fmt"
	"os"
	"budgetchain/x/budget/types"
)

// Chain keeps only an append-only block history. Identity, signature, m-of-n,
// and tax logic live in Keeper and Contract so the blockchain layer remains
// separate from business rules.
type Chain struct {
	Name   string
	Blocks []*types.Block
}

// New creates a ledger genesis block with its initial StateRoot. For example,
// BudgetPrivate and TaxPrivate have independent genesis blocks and state roots.
func New(name string, stateRoot []byte) *Chain {
	return &Chain{
		Name:   name,
		Blocks: []*types.Block{types.GenesisBlock(stateRoot)},
	}
}

// Tip returns the latest trusted block in the chain.
func (c *Chain) Tip() *types.Block {
	return c.Blocks[len(c.Blocks)-1]
}

// Commit converts successful transactions into a new block. The primary node
// validates contract rules and signatures first, so no invalid or empty block is
// created after genesis.
func (c *Chain) Commit(txs []types.Tx, stateRoot []byte) (*types.Block, error) {
	if len(txs) == 0 {
		return nil, fmt.Errorf("empty block is not allowed after genesis")
	}
	tip := c.Tip()
	b := types.NewBlock(tip.Header.Height+1, tip.Hash, stateRoot, txs)
	c.Blocks = append(c.Blocks, b)

	// ---  پرینت در کنسول ---
	fmt.Printf("=> [COMMIT] Ledger: %-13s | Height: %d | Txs: %d\n", c.Name, b.Header.Height, len(txs))

	// ---  نوشتن لاگ در فایل اختصاصی هر لجر ---
	fileName := fmt.Sprintf("%s.log", c.Name) // مثلاً BudgetPrivate.log
	
	// باز کردن فایل در حالت Append (اگر نبود ساخته شود)
	file, err := os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		defer file.Close()
		
		// نوشتن هدر بلاک
		file.WriteString(fmt.Sprintf("=== Block %d | Hash: %x ===\n", b.Header.Height, b.Hash[:8]))
		
		// نوشتن جزئیات تک‌تک تراکنش‌های داخل بلاک
		for i, tx := range txs {
			// `%T` نوع تراکنش (مثلا TxBudgetDisclosure) و `%+v` مقادیر داخل آن را چاپ می‌کند
			file.WriteString(fmt.Sprintf("  Tx [%d]: Type: %T \n  Data: %+v\n\n", i, tx, tx))
		}
		file.WriteString("------------------------------------------------------\n")
	} else {
		fmt.Printf("Error writing to file %s: %v\n", fileName, err)
	}

	return b, nil
}
// Verify checks ledger structural integrity: genesis, height progression,
// prev_hash linkage, transaction roots, and block hashes. Simulated validators
// use the same checks for new blocks.
func (c *Chain) Verify() error {
	if len(c.Blocks) == 0 {
		return fmt.Errorf("empty chain")
	}
	g := c.Blocks[0]
	if g.Header.Height != 0 {
		return fmt.Errorf("genesis height must be 0")
	}
	if !bytes.Equal(g.Header.PrevHash, make([]byte, 32)) {
		return fmt.Errorf("genesis prev_hash must be 32 zero bytes")
	}
	if !bytes.Equal(g.Hash, types.ComputeBlockHash(g.Header)) {
		return fmt.Errorf("genesis hash mismatch")
	}

	for i := 1; i < len(c.Blocks); i++ {
		cur, prev := c.Blocks[i], c.Blocks[i-1]
		if cur.Header.Height != prev.Header.Height+1 {
			return fmt.Errorf("height gap at %d", i)
		}
		if !bytes.Equal(cur.Header.PrevHash, prev.Hash) {
			return fmt.Errorf("broken link at height %d", cur.Header.Height)
		}
		if !bytes.Equal(cur.Header.TxRoot, types.TxRoot(cur.Txs)) {
			return fmt.Errorf("tx_root mismatch at height %d", cur.Header.Height)
		}
		if !bytes.Equal(cur.Hash, types.ComputeBlockHash(cur.Header)) {
			return fmt.Errorf("hash mismatch at height %d", cur.Header.Height)
		}
	}
	return nil
}

// Height returns the tip height for displaying the state of each ledger.
func (c *Chain) Height() uint64 {
	return c.Tip().Header.Height
}
