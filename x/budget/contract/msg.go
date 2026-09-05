package contract

import (
	"fmt"
	"strings"

	"budgetchain/x/budget/types"
)

// Msg is a deterministic contract call in BudgetPrivate. ValidateBasic checks
// only the message shape; the Keeper verifies the signature and sender role.
type Msg interface {
	Type() string
	GetSender() string
	ValidateBasic() error
}

// MsgUpsertPolicy is a signed policy-administrator message that creates or
// versions an m-of-n approval policy.
type MsgUpsertPolicy struct {
	ActorID   string
	Policy    types.ApprovalPolicy
	Signature string
}

func (m MsgUpsertPolicy) Type() string      { return "upsert_policy" }
func (m MsgUpsertPolicy) GetSender() string { return m.ActorID }
func (m MsgUpsertPolicy) ValidateBasic() error {
	if strings.TrimSpace(m.ActorID) == "" || strings.TrimSpace(m.Signature) == "" || strings.TrimSpace(m.Policy.ID) == "" {
		return fmt.Errorf("%w: policy actor, id, and signature are required", ErrInvalidMsg)
	}
	return nil
}

// MsgSubmitBudgetRequest carries the requesting unit's signed request and the
// minimum tax-status snapshot to the contract. Its full data is not public.
type MsgSubmitBudgetRequest struct {
	Input     types.BudgetRequestInput
	Signature string
	TaxStatus types.TaxStatusSnapshot
}

func (m MsgSubmitBudgetRequest) Type() string      { return "submit_budget_request" }
func (m MsgSubmitBudgetRequest) GetSender() string { return m.Input.OrganizationID }
func (m MsgSubmitBudgetRequest) ValidateBasic() error {
	if strings.TrimSpace(m.Input.OrganizationID) == "" || strings.TrimSpace(m.Signature) == "" {
		return fmt.Errorf("%w: request organization and signature are required", ErrInvalidMsg)
	}
	return nil
}

// MsgApproveBudgetRequest carries one approver's signature for a specific
// request. The signature is also stored in the private transaction.
type MsgApproveBudgetRequest struct {
	RequestID  string
	ApproverID string
	Signature  string
}

func (m MsgApproveBudgetRequest) Type() string      { return "approve_budget_request" }
func (m MsgApproveBudgetRequest) GetSender() string { return m.ApproverID }
func (m MsgApproveBudgetRequest) ValidateBasic() error {
	if strings.TrimSpace(m.RequestID) == "" || strings.TrimSpace(m.ApproverID) == "" || strings.TrimSpace(m.Signature) == "" {
		return fmt.Errorf("%w: request id, approver, and signature are required", ErrInvalidMsg)
	}
	return nil
}
