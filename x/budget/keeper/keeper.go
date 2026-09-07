// Package keeper keeps the private budget-network state in memory. It is the
// core that applies the identity registry, m-of-n policy, requests, and budget
// release together; this package's data must not be published directly.
package keeper

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"budgetchain/x/budget/types"
)

var (
	ErrIdentityNotFound     = errors.New("identity not found")
	ErrIdentityExists       = errors.New("identity already registered")
	ErrIdentityInactive     = errors.New("identity is inactive")
	ErrRoleRequired         = errors.New("identity does not have the required role")
	ErrPolicyNotFound       = errors.New("approval policy not found")
	ErrInvalidPolicy        = errors.New("invalid approval policy")
	ErrPolicyVersion        = errors.New("invalid approval policy version")
	ErrRequestNotFound      = errors.New("budget request not found")
	ErrRequestExists        = errors.New("client reference was already submitted by this organization")
	ErrAlreadyReleased      = errors.New("budget request is already released")
	ErrNotReleased          = errors.New("budget request is not released")
	ErrNotApprover          = errors.New("signer is not allowed by this request policy")
	ErrDuplicateApproval    = errors.New("this organization already approved the request")
	ErrInvalidRequest       = errors.New("invalid budget request")
	ErrTaxStatusUnavailable = errors.New("a current tax status is required before budget submission")
)

type Keeper struct {
	// Every map is changed only while this Keeper lock is held. A production
	// version must move this state to persistent storage and real consensus.
	mu               sync.RWMutex
	identities       map[string]*types.Identity
	policies         map[string]*types.ApprovalPolicy
	requests         map[string]*types.BudgetRequest
	transfers        map[string]*types.CreditTransfer
	requestReference map[string]string
	requestSeq       uint64
	transferSeq      uint64
}

func NewKeeper() *Keeper {
	return &Keeper{
		identities:       make(map[string]*types.Identity),
		policies:         make(map[string]*types.ApprovalPolicy),
		requests:         make(map[string]*types.BudgetRequest),
		transfers:        make(map[string]*types.CreditTransfer),
		requestReference: make(map[string]string),
	}
}

// RegisterIdentity is the primary node's administrative operation for
// registering an organizational unit's public key. Private keys are stored in
// neither the registry nor blocks, so requests are accepted by signature proof,
// not by trusting a textual name.
func (k *Keeper) RegisterIdentity(identity types.Identity) (*types.Identity, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	if err := validateIdentity(identity); err != nil {
		return nil, err
	}
	if _, exists := k.identities[identity.ID]; exists {
		return nil, ErrIdentityExists
	}
	identity.Roles = append([]types.Role(nil), identity.Roles...)
	k.identities[identity.ID] = &identity
	return cloneIdentity(&identity), nil
}

// GetIdentity returns a copy so callers cannot modify registry state outside the
// Keeper lock.
func (k *Keeper) GetIdentity(id string) (*types.Identity, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	identity, ok := k.identities[id]
	if !ok {
		return nil, ErrIdentityNotFound
	}
	return cloneIdentity(identity), nil
}

// UpsertPolicy creates or changes an m-of-n policy only with a POLICY_ADMIN
// signature. Version numbers must be sequential, and recorded requests retain
// snapshots so they are unaffected by later changes.
func (k *Keeper) UpsertPolicy(actorID string, policy types.ApprovalPolicy, signature string) (*types.ApprovalPolicy, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	actor, err := k.activeIdentityWithRoleLocked(actorID, types.RolePolicyAdmin)
	if err != nil {
		return nil, err
	}
	if actorID != policy.OwnerID {
		return nil, ErrRoleRequired
	}
	if err := validatePolicy(policy); err != nil {
		return nil, err
	}
	if existing, exists := k.policies[policy.ID]; exists {
		if existing.OwnerID != actorID || policy.Version != existing.Version+1 {
			return nil, ErrPolicyVersion
		}
	} else if policy.Version != 1 {
		return nil, ErrPolicyVersion
	}
	if err := types.VerifySignature(actor.PublicKey, policy.SigningBytes(), signature); err != nil {
		return nil, err
	}

	for _, approverID := range policy.ApproverIDs {
		if _, err := k.activeIdentityWithRoleLocked(approverID, types.RoleApprover); err != nil {
			return nil, fmt.Errorf("approver %q: %w", approverID, err)
		}
	}
	policy.ApproverIDs = append([]string(nil), policy.ApproverIDs...)
	policy.UpdatedAt = time.Now().UTC()
	k.policies[policy.ID] = &policy
	return clonePolicy(&policy), nil
}

// SubmitSignedRequest joins three core claims in one operation: it reads the
// requester identity from the registry, verifies the signature with that public
// key, and receives only a minimal tax-status snapshot from TaxPrivate. Its
// organization-plus-client-reference check runs before this method creates a
// request or changes state; therefore ErrRequestExists reaches Node before a
// BudgetPrivate transaction or block can be committed.
func (k *Keeper) SubmitSignedRequest(input types.BudgetRequestInput, signature string, taxStatus types.TaxStatusSnapshot) (*types.BudgetRequest, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	//validateRequest raises error if the request doesnt have it's necessary arguments.
	if err := validateRequest(input); err != nil {
		return nil, err
	}
	//if it's is not among other ids or it doesnt have roll it shows an error
	requester, err := k.activeIdentityWithRoleLocked(input.OrganizationID, types.RoleRequester)
	if err != nil {
		return nil, err
	}
	if err := types.VerifySignature(requester.PublicKey, input.SigningBytes(), signature); err != nil {
		return nil, err
	}
	policy, ok := k.policies[input.PolicyID]
	if !ok {
		return nil, ErrPolicyNotFound
	}
	if taxStatus.OrganizationID != input.OrganizationID || taxStatus.Status == types.TaxStatusUnknown || taxStatus.ValidUntil.Before(time.Now().UTC()) || taxStatus.AttestationCommitment == "" {
		return nil, ErrTaxStatusUnavailable
	}
	refKey := input.OrganizationID + "\x00" + input.ClientReference
	if _, exists := k.requestReference[refKey]; exists {
		// The in-memory index is rebuilt from committed BudgetPrivate history when
		// a persistent node is opened, so this is not RAM-only protection.
		return nil, ErrRequestExists
	}
	// The public reference and nonce are independent of request content. Publishing
	// a simple amount-and-purpose hash would let an outsider recreate it by guessing.
	publicReference, err := types.NewOpaqueToken("budget-public")
	if err != nil {
		return nil, err
	}
	disclosureNonce, err := types.NewOpaqueToken("budget-nonce")
	if err != nil {
		return nil, err
	}

	k.requestSeq++
	now := time.Now().UTC()
	request := &types.BudgetRequest{
		ID:                 fmt.Sprintf("req-%06d", k.requestSeq),
		Input:              input,
		RequesterSignature: types.Signature{SignerID: input.OrganizationID, Value: signature},
		RequestCommitment:  types.DigestHex(input.SigningBytes()),
		Policy:             *clonePolicy(policy),
		TaxStatus:          taxStatus,
		Approvals:          []types.Approval{},
		Status:             types.StatusPending,
		SubmittedAt:        now,
		PublicReference:    publicReference,
		DisclosureNonce:    disclosureNonce,
	}
	k.requests[request.ID] = request
	k.requestReference[refKey] = request.ID
	return cloneRequest(request), nil
}

// Approve verifies a registered approver signature. When valid signatures reach
// m, the request changes to RELEASED and a CreditTransfer is created in one
// logical transaction, making release without the threshold impossible.
func (k *Keeper) Approve(requestID, approverID, signature string) (*types.BudgetRequest, *types.CreditTransfer, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	request, ok := k.requests[requestID]
	if !ok {
		return nil, nil, ErrRequestNotFound
	}
	if request.Status == types.StatusReleased {
		return nil, nil, ErrAlreadyReleased
	}
	if !contains(request.Policy.ApproverIDs, approverID) {
		return nil, nil, ErrNotApprover
	}
	for _, approval := range request.Approvals {
		if approval.SignerID == approverID {
			return nil, nil, ErrDuplicateApproval
		}
	}
	approver, err := k.activeIdentityWithRoleLocked(approverID, types.RoleApprover)
	if err != nil {
		return nil, nil, err
	}
	if err := types.VerifySignature(approver.PublicKey, types.ApprovalSigningBytes(request.ID, request.RequestCommitment, request.Policy.Version), signature); err != nil {
		return nil, nil, err
	}

	request.Approvals = append(request.Approvals, types.Approval{
		Signature: types.Signature{SignerID: approverID, Value: signature},
		SignedAt:  time.Now().UTC(),
	})
	var transfer *types.CreditTransfer
	if len(request.Approvals) >= request.Policy.ThresholdM {
		now := time.Now().UTC()
		request.Status = types.StatusReleased
		request.ReleasedAt = &now
		k.transferSeq++
		transfer = &types.CreditTransfer{
			ID:          fmt.Sprintf("transfer-%06d", k.transferSeq),
			RequestID:   request.ID,
			RecipientID: request.Input.OrganizationID,
			Amount:      request.Input.Amount,
			ExecutedAt:  now,
		}
		transfer.Reference = types.DigestHex(types.MustJSON(struct {
			RequestID   string    `json:"request_id"`
			RecipientID string    `json:"recipient_id"`
			Amount      uint64    `json:"amount"`
			ExecutedAt  time.Time `json:"executed_at"`
		}{transfer.RequestID, transfer.RecipientID, transfer.Amount, transfer.ExecutedAt}))
		k.transfers[transfer.ID] = transfer
		request.CreditTransferID = transfer.ID
	}
	return cloneRequest(request), cloneTransfer(transfer), nil
}

// GetRequestForAuditor returns the complete request only to an AUDITOR identity.
// This path is intentionally separate from public disclosure so amounts and
// signatures remain private.
func (k *Keeper) GetRequestForAuditor(callerID, requestID string) (*types.BudgetRequest, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if _, err := k.activeIdentityWithRoleLocked(callerID, types.RoleAuditor); err != nil {
		return nil, err
	}
	request, ok := k.requests[requestID]
	if !ok {
		return nil, ErrRequestNotFound
	}
	return cloneRequest(request), nil
}

// GetTransferForAuditor exposes budget-release details only to an authorized
// auditor within the private network.
func (k *Keeper) GetTransferForAuditor(callerID, transferID string) (*types.CreditTransfer, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if _, err := k.activeIdentityWithRoleLocked(callerID, types.RoleAuditor); err != nil {
		return nil, err
	}
	transfer, ok := k.transfers[transferID]
	if !ok {
		return nil, ErrRequestNotFound
	}
	return cloneTransfer(transfer), nil
}

// BuildPublicDisclosure creates a minimal pseudonymous event for BudgetPublic
// only after RELEASED. It returns neither the amount nor the organizational
// name, purpose, policy, or approver signatures.
func (k *Keeper) BuildPublicDisclosure(requestID string) (*types.PublicBudgetRecord, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	request, ok := k.requests[requestID]
	if !ok {
		return nil, ErrRequestNotFound
	}
	if request.Status != types.StatusReleased || request.ReleasedAt == nil {
		return nil, ErrNotReleased
	}
	// Creating a public record must not alter private state. The release block's
	// StateRoot therefore refers to the same private transition. The nonce stays in
	// BudgetPrivate and protects the public commitment from data guessing.
	publicCommitment := types.DigestHex(types.MustJSON(struct {
		Domain            string    `json:"domain"`
		PublicReference   string    `json:"public_reference"`
		RequestCommitment string    `json:"request_commitment"`
		DisclosureNonce   string    `json:"disclosure_nonce"`
		Status            string    `json:"status"`
		ReleasedAt        time.Time `json:"released_at"`
	}{"budget-public-event:v2", request.PublicReference, request.RequestCommitment, request.DisclosureNonce, request.Status, *request.ReleasedAt}))
	return &types.PublicBudgetRecord{
		PublicReference:      request.PublicReference,
		Status:               request.Status,
		ReleasedAt:           *request.ReleasedAt,
		PublishedAt:          time.Now().UTC(),
		DisclosureCommitment: publicCommitment,
	}, nil
}

// StateRoot hashes all private state and records it in the block header. Any
// registry, policy, request, or transfer change therefore links the chain to a
// new state.
func (k *Keeper) StateRoot() []byte {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return types.JSONDigest(struct {
		Identities       map[string]*types.Identity       `json:"identities"`
		Policies         map[string]*types.ApprovalPolicy `json:"policies"`
		Requests         map[string]*types.BudgetRequest  `json:"requests"`
		Transfers        map[string]*types.CreditTransfer `json:"transfers"`
		RequestReference map[string]string                `json:"request_reference"`
		RequestSeq       uint64                           `json:"request_seq"`
		TransferSeq      uint64                           `json:"transfer_seq"`
	}{k.identities, k.policies, k.requests, k.transfers, k.requestReference, k.requestSeq, k.transferSeq})
}

// activeIdentityWithRoleLocked checks both identity activation and the required
// role. This is the permissioned-network control at the business-logic layer.
func (k *Keeper) activeIdentityWithRoleLocked(id string, role types.Role) (*types.Identity, error) {
	identity, ok := k.identities[id]
	if !ok {
		return nil, ErrIdentityNotFound
	}
	if !identity.Active {
		return nil, ErrIdentityInactive
	}
	if !identity.HasRole(role) {
		return nil, ErrRoleRequired
	}
	return identity, nil
}

func validateIdentity(identity types.Identity) error {
	if strings.TrimSpace(identity.ID) == "" || strings.TrimSpace(identity.DisplayName) == "" || strings.TrimSpace(identity.PublicKey) == "" || len(identity.Roles) == 0 {
		return ErrInvalidRequest
	}
	if _, err := types.DecodePublicKey(identity.PublicKey); err != nil {
		return err
	}
	return nil
}

func validatePolicy(policy types.ApprovalPolicy) error {
	if strings.TrimSpace(policy.ID) == "" || strings.TrimSpace(policy.OwnerID) == "" || len(policy.ApproverIDs) == 0 || policy.ThresholdM < 1 || policy.ThresholdM > len(policy.ApproverIDs) || hasDuplicates(policy.ApproverIDs) {
		return ErrInvalidPolicy
	}
	return nil
}

func validateRequest(input types.BudgetRequestInput) error {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ClientReference) == "" || strings.TrimSpace(input.BudgetProgram) == "" || strings.TrimSpace(input.Purpose) == "" || strings.TrimSpace(input.PolicyID) == "" || input.FiscalYear == 0 || input.Amount == 0 {
		return ErrInvalidRequest
	}
	return nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func hasDuplicates(values []string) bool {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			return true
		}
		seen[value] = true
	}
	return false
}

func cloneIdentity(identity *types.Identity) *types.Identity {
	if identity == nil {
		return nil
	}
	copy := *identity
	copy.Roles = append([]types.Role(nil), identity.Roles...)
	return &copy
}

func clonePolicy(policy *types.ApprovalPolicy) *types.ApprovalPolicy {
	if policy == nil {
		return nil
	}
	copy := *policy
	copy.ApproverIDs = append([]string(nil), policy.ApproverIDs...)
	return &copy
}

func cloneRequest(request *types.BudgetRequest) *types.BudgetRequest {
	if request == nil {
		return nil
	}
	cloned := *request
	cloned.Policy = *clonePolicy(&request.Policy)
	if request.Approvals != nil {
		cloned.Approvals = make([]types.Approval, len(request.Approvals))
		copy(cloned.Approvals, request.Approvals)
	}
	if request.ReleasedAt != nil {
		releasedAt := *request.ReleasedAt
		cloned.ReleasedAt = &releasedAt
	}
	return &cloned
}

func cloneTransfer(transfer *types.CreditTransfer) *types.CreditTransfer {
	if transfer == nil {
		return nil
	}
	copy := *transfer
	return &copy
}
