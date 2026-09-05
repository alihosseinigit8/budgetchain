package node

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"budgetchain/x/budget/chain"
	"budgetchain/x/budget/contract"
	"budgetchain/x/budget/keeper"
	"budgetchain/x/budget/types"
	taxkeeper "budgetchain/x/tax/keeper"
)

const persistedStateVersion = 1

// persistedNode is the durable node image. The request-reference index is not
// serialized as authoritative state; it is reconstructed from BudgetPrivate
// transactions whenever the node is opened.
type persistedNode struct {
	Version             int                        `json:"version"`
	BudgetPrivate       *chain.Chain               `json:"budget_private"`
	BudgetPublic        *chain.Chain               `json:"budget_public"`
	TaxPrivate          *chain.Chain               `json:"tax_private"`
	TaxPublic           *chain.Chain               `json:"tax_public"`
	BudgetState         keeper.Snapshot            `json:"budget_state"`
	TaxState            taxkeeper.Snapshot         `json:"tax_state"`
	PublicBudgetRecords []types.PublicBudgetRecord `json:"public_budget_records"`
	PublicTaxRecords    []types.PublicTaxRecord    `json:"public_tax_records"`
}

// Open creates a persistent node backed by storagePath. On restart, it verifies
// all four block histories, restores state, and rebuilds duplicate-request
// protection from BudgetPrivate history rather than trusting an in-memory map.
func Open(storagePath string) (*Node, error) {
	storagePath = strings.TrimSpace(storagePath)
	if storagePath == "" {
		return nil, fmt.Errorf("storage path is required")
	}

	raw, err := os.ReadFile(storagePath)
	if errors.Is(err, os.ErrNotExist) {
		n := New()
		n.storagePath = storagePath
		if err := n.persist(); err != nil {
			return nil, err
		}
		return n, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read persistent node state: %w", err)
	}

	var stored persistedNode
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, fmt.Errorf("decode persistent node state: %w", err)
	}
	if stored.Version != persistedStateVersion {
		return nil, fmt.Errorf("unsupported persistent node state version %d", stored.Version)
	}
	return restorePersistentNode(storagePath, stored)
}

func restorePersistentNode(storagePath string, stored persistedNode) (*Node, error) {
	if stored.BudgetPrivate == nil || stored.BudgetPublic == nil || stored.TaxPrivate == nil || stored.TaxPublic == nil {
		return nil, fmt.Errorf("persistent node state is missing a ledger")
	}
	for name, ledger := range map[string]*chain.Chain{
		"budget-private": stored.BudgetPrivate,
		"budget-public":  stored.BudgetPublic,
		"tax-private":    stored.TaxPrivate,
		"tax-public":     stored.TaxPublic,
	} {
		if err := ledger.Verify(); err != nil {
			return nil, fmt.Errorf("verify persisted %s ledger: %w", name, err)
		}
	}

	budgetState, err := keeper.NewKeeperFromSnapshot(stored.BudgetState)
	if err != nil {
		return nil, fmt.Errorf("restore budget state: %w", err)
	}
	references, err := requestReferencesFromHistory(stored.BudgetPrivate)
	if err != nil {
		return nil, err
	}
	if err := budgetState.RebuildRequestReferences(references); err != nil {
		return nil, fmt.Errorf("rebuild duplicate-request index: %w", err)
	}
	taxState, err := taxkeeper.NewKeeperFromSnapshot(stored.TaxState)
	if err != nil {
		return nil, fmt.Errorf("restore tax state: %w", err)
	}

	if !bytes.Equal(stored.BudgetPrivate.Tip().Header.StateRoot, budgetState.StateRoot()) {
		return nil, fmt.Errorf("budget-private state root does not match restored state")
	}
	if !bytes.Equal(stored.TaxPrivate.Tip().Header.StateRoot, taxState.StateRoot()) {
		return nil, fmt.Errorf("tax-private state root does not match restored state")
	}
	if !bytes.Equal(stored.BudgetPublic.Tip().Header.StateRoot, types.JSONDigest(stored.PublicBudgetRecords)) {
		return nil, fmt.Errorf("budget-public state root does not match restored records")
	}
	if !bytes.Equal(stored.TaxPublic.Tip().Header.StateRoot, types.JSONDigest(stored.PublicTaxRecords)) {
		return nil, fmt.Errorf("tax-public state root does not match restored records")
	}

	return &Node{
		Keeper:              budgetState,
		TaxKeeper:           taxState,
		Contract:            contract.New(budgetState),
		BudgetPrivate:       stored.BudgetPrivate,
		BudgetPublic:        stored.BudgetPublic,
		TaxPrivate:          stored.TaxPrivate,
		TaxPublic:           stored.TaxPublic,
		Chain:               stored.BudgetPrivate,
		budgetPublicRecords: clonePublicBudgetRecords(stored.PublicBudgetRecords),
		taxPublicRecords:    clonePublicTaxRecords(stored.PublicTaxRecords),
		simulatedValidators: make(map[string]*SimulatedValidator),
		storagePath:         storagePath,
	}, nil
}

// requestReferencesFromHistory extracts the only duplicate-request keys trusted
// after restart: each key must originate in a committed BudgetPrivate request Tx.
func requestReferencesFromHistory(ledger *chain.Chain) (map[string]string, error) {
	references := make(map[string]string)
	for blockIndex, block := range ledger.Blocks {
		if blockIndex == 0 {
			continue
		}
		for _, tx := range block.Txs {
			if tx.Type != types.TxBudgetRequest {
				continue
			}
			var request types.BudgetRequest
			if err := json.Unmarshal(tx.Payload, &request); err != nil {
				return nil, fmt.Errorf("decode request at budget-private height %d: %w", block.Header.Height, err)
			}
			if request.ID == "" || request.Input.OrganizationID == "" || request.Input.ClientReference == "" {
				return nil, fmt.Errorf("invalid request at budget-private height %d", block.Header.Height)
			}
			refKey := request.Input.OrganizationID + "\x00" + request.Input.ClientReference
			if existingID, exists := references[refKey]; exists {
				return nil, fmt.Errorf("duplicate request reference in history: %q and %q", existingID, request.ID)
			}
			references[refKey] = request.ID
		}
	}
	return references, nil
}

func (n *Node) persist() error {
	if n.storagePath == "" {
		return nil
	}
	n.persistMu.Lock()
	defer n.persistMu.Unlock()

	n.mu.RLock()
	publicBudgetRecords := clonePublicBudgetRecords(n.budgetPublicRecords)
	publicTaxRecords := clonePublicTaxRecords(n.taxPublicRecords)
	n.mu.RUnlock()

	stored := persistedNode{
		Version:             persistedStateVersion,
		BudgetPrivate:       n.BudgetPrivate,
		BudgetPublic:        n.BudgetPublic,
		TaxPrivate:          n.TaxPrivate,
		TaxPublic:           n.TaxPublic,
		BudgetState:         n.Keeper.Snapshot(),
		TaxState:            n.TaxKeeper.Snapshot(),
		PublicBudgetRecords: publicBudgetRecords,
		PublicTaxRecords:    publicTaxRecords,
	}
	raw, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("encode persistent node state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(n.storagePath), 0o700); err != nil {
		return fmt.Errorf("create persistent node directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(n.storagePath), ".budgetchain-*.tmp")
	if err != nil {
		return fmt.Errorf("create persistent node state file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("protect persistent node state file: %w", err)
	}
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return fmt.Errorf("write persistent node state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close persistent node state: %w", err)
	}
	if err := os.Rename(temporaryPath, n.storagePath); err != nil {
		return fmt.Errorf("replace persistent node state: %w", err)
	}
	return nil
}

func clonePublicBudgetRecords(records []types.PublicBudgetRecord) []types.PublicBudgetRecord {
	if records == nil {
		return nil
	}
	cloned := make([]types.PublicBudgetRecord, len(records))
	copy(cloned, records)
	return cloned
}

func clonePublicTaxRecords(records []types.PublicTaxRecord) []types.PublicTaxRecord {
	if records == nil {
		return nil
	}
	cloned := make([]types.PublicTaxRecord, len(records))
	copy(cloned, records)
	return cloned
}
