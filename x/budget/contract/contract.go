// Package contract executes the deterministic rules of the private budget
// network. It is the PoC's smart-contract layer, not an EVM implementation or
// a live smart-contract platform.
package contract

import (
	"budgetchain/x/budget/keeper"
	"budgetchain/x/budget/types"
)

// Contract executes rules and delegates state ownership to the Keeper. This
// separation rejects invalid messages before a block is created.
type Contract struct {
	k *keeper.Keeper
}

// Event is the observable result of rule execution. In this version, contract
// events contain no sensitive fields; complete payloads stay private.
type Event struct {
	Type  string
	Attrs map[string]string
}

// ExecResult returns a successful state transition to the primary node, which
// records it in the appropriate transaction for one of the four ledgers.
type ExecResult struct {
	Policy   *types.ApprovalPolicy
	Request  *types.BudgetRequest
	Transfer *types.CreditTransfer
	Events   []Event
}

// New connects the contract to BudgetPrivate state, not to TaxPrivate or either
// public ledger.
func New(k *keeper.Keeper) *Contract {
	return &Contract{k: k}
}

// Execute is the contract's deterministic gateway: it validates the message
// shape and then runs one of the project's three permitted rules.
func (c *Contract) Execute(msg Msg) (*ExecResult, error) {
	if c == nil || c.k == nil {
		return nil, ErrNilKeeper
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	switch m := msg.(type) {
	case MsgUpsertPolicy:
		return c.upsertPolicy(m)
	case MsgSubmitBudgetRequest:
		return c.submitRequest(m)
	case MsgApproveBudgetRequest:
		return c.approveRequest(m)
	default:
		return nil, ErrUnknownMsg
	}
}

// upsertPolicy delegates versioned m-of-n policy changes to the Keeper, which
// verifies the policy owner, administrator role, and signature.
func (c *Contract) upsertPolicy(m MsgUpsertPolicy) (*ExecResult, error) {
	policy, err := c.k.UpsertPolicy(m.ActorID, m.Policy, m.Signature)
	if err != nil {
		return nil, err
	}
	return &ExecResult{Policy: policy, Events: []Event{{
		Type:  "approval_policy_updated",
		Attrs: map[string]string{"policy_id": policy.ID, "version": formatUint(policy.Version)},
	}}}, nil
}

// submitRequest creates a BudgetPrivate request only after the requesting
// unit's signature and a current tax snapshot have been verified.
func (c *Contract) submitRequest(m MsgSubmitBudgetRequest) (*ExecResult, error) {
	request, err := c.k.SubmitSignedRequest(m.Input, m.Signature, m.TaxStatus)
	if err != nil {
		return nil, err
	}
	return &ExecResult{Request: request, Events: []Event{{
		Type:  "budget_request_submitted",
		Attrs: map[string]string{"request_id": request.ID, "status": request.Status},
	}}}, nil
}

// approveRequest delegates valid-signature counting to the Keeper. If this is
// the threshold signature, it returns the transfer so the primary node can
// record it atomically with the state transition.
func (c *Contract) approveRequest(m MsgApproveBudgetRequest) (*ExecResult, error) {
	request, transfer, err := c.k.Approve(m.RequestID, m.ApproverID, m.Signature)
	if err != nil {
		return nil, err
	}
	eventType := "budget_request_approved"
	if transfer != nil {
		eventType = "credit_transfer_released"
	}
	return &ExecResult{Request: request, Transfer: transfer, Events: []Event{{
		Type:  eventType,
		Attrs: map[string]string{"request_id": request.ID, "status": request.Status},
	}}}, nil
}
