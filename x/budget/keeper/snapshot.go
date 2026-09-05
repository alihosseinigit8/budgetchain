package keeper

import (
	"fmt"

	"budgetchain/x/budget/types"
)

// Snapshot is the serializable private-budget state required to resume a node.
// RequestReference is deliberately excluded: it is rebuilt from BudgetPrivate
// history when persistent state is opened again.
type Snapshot struct {
	Identities  map[string]*types.Identity       `json:"identities"`
	Policies    map[string]*types.ApprovalPolicy `json:"policies"`
	Requests    map[string]*types.BudgetRequest  `json:"requests"`
	Transfers   map[string]*types.CreditTransfer `json:"transfers"`
	RequestSeq  uint64                           `json:"request_seq"`
	TransferSeq uint64                           `json:"transfer_seq"`
}

// Snapshot returns a detached serializable copy of private-budget state.
func (k *Keeper) Snapshot() Snapshot {
	k.mu.RLock()
	defer k.mu.RUnlock()

	snapshot := Snapshot{
		Identities:  make(map[string]*types.Identity, len(k.identities)),
		Policies:    make(map[string]*types.ApprovalPolicy, len(k.policies)),
		Requests:    make(map[string]*types.BudgetRequest, len(k.requests)),
		Transfers:   make(map[string]*types.CreditTransfer, len(k.transfers)),
		RequestSeq:  k.requestSeq,
		TransferSeq: k.transferSeq,
	}
	for id, identity := range k.identities {
		snapshot.Identities[id] = cloneIdentity(identity)
	}
	for id, policy := range k.policies {
		snapshot.Policies[id] = clonePolicy(policy)
	}
	for id, request := range k.requests {
		snapshot.Requests[id] = cloneRequest(request)
	}
	for id, transfer := range k.transfers {
		snapshot.Transfers[id] = cloneTransfer(transfer)
	}
	return snapshot
}

// NewKeeperFromSnapshot restores private-budget state. RequestReference remains
// empty until RebuildRequestReferences verifies and rebuilds it from ledger
// history.
func NewKeeperFromSnapshot(snapshot Snapshot) (*Keeper, error) {
	k := NewKeeper()
	k.requestSeq = snapshot.RequestSeq
	k.transferSeq = snapshot.TransferSeq

	for id, identity := range snapshot.Identities {
		if identity == nil || id == "" || identity.ID != id {
			return nil, fmt.Errorf("invalid persisted identity %q", id)
		}
		k.identities[id] = cloneIdentity(identity)
	}
	for id, policy := range snapshot.Policies {
		if policy == nil || id == "" || policy.ID != id {
			return nil, fmt.Errorf("invalid persisted policy %q", id)
		}
		k.policies[id] = clonePolicy(policy)
	}
	for id, request := range snapshot.Requests {
		if request == nil || id == "" || request.ID != id {
			return nil, fmt.Errorf("invalid persisted request %q", id)
		}
		k.requests[id] = cloneRequest(request)
	}
	for id, transfer := range snapshot.Transfers {
		if transfer == nil || id == "" || transfer.ID != id {
			return nil, fmt.Errorf("invalid persisted transfer %q", id)
		}
		k.transfers[id] = cloneTransfer(transfer)
	}
	return k, nil
}

// RebuildRequestReferences replaces the in-memory duplicate-request index with
// the references extracted from BudgetPrivate history. Each persisted request
// must have exactly one matching historical request transaction.
func (k *Keeper) RebuildRequestReferences(references map[string]string) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if len(references) != len(k.requests) {
		return fmt.Errorf("request history/state count mismatch")
	}
	for refKey, requestID := range references {
		request, ok := k.requests[requestID]
		if !ok || request == nil {
			return fmt.Errorf("history references unknown request %q", requestID)
		}
		expectedKey := request.Input.OrganizationID + "\x00" + request.Input.ClientReference
		if refKey != expectedKey {
			return fmt.Errorf("history reference mismatch for request %q", requestID)
		}
	}

	k.requestReference = make(map[string]string, len(references))
	for refKey, requestID := range references {
		k.requestReference[refKey] = requestID
	}
	return nil
}
