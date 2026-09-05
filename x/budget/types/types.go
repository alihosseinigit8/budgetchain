// Package types contains the shared data models for the PoC's four ledgers.
// Keeping these models separate makes the private-versus-publishable data
// boundary explicit in code, not only in project documentation.
package types

import (
	"encoding/json"
	"time"
)

const (
	StatusPending  = "PENDING"
	StatusReleased = "RELEASED"
)

// Role defines the permitted function of a registered public key in the
// private budget network. In this PoC, each key belongs to an organizational
// unit, so anonymous requests and approvals are never accepted.
type Role string

const (
	RoleRequester    Role = "REQUESTER"
	RoleApprover     Role = "APPROVER"
	RolePolicyAdmin  Role = "POLICY_ADMIN"
	RoleAuditor      Role = "AUDITOR"
	RoleTaxAuthority Role = "TAX_AUTHORITY"
)

type Identity struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	// PublicKey is an ECDSA public key encoded as DER and Base64. The private key
	// is deliberately absent from this structure, the registry, and all blocks.
	PublicKey string `json:"public_key"`
	Roles     []Role `json:"roles"`
	Active    bool   `json:"active"`
}

func (i Identity) HasRole(role Role) bool {
	for _, current := range i.Roles {
		if current == role {
			return true
		}
	}
	return false
}

// Signature is stored beside the message it authorizes in a private-network
// transaction. Value is an ASN.1 ECDSA signature encoded as Base64; including
// it in Tx commits it to the transaction hash and therefore the block TxRoot.
type Signature struct {
	SignerID string `json:"signer_id"`
	Value    string `json:"value"`
}

// ApprovalPolicy is a mutable m-of-n policy. Each request stores a snapshot at
// submission time so later policy changes cannot retroactively alter an
// in-progress request.
type ApprovalPolicy struct {
	ID          string    `json:"id"`
	OwnerID     string    `json:"owner_id"`
	Version     uint64    `json:"version"`
	ThresholdM  int       `json:"threshold_m"`
	ApproverIDs []string  `json:"approver_ids"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (p ApprovalPolicy) SigningBytes() []byte {
	return canonicalJSON(struct {
		Domain      string   `json:"domain"`
		ID          string   `json:"id"`
		OwnerID     string   `json:"owner_id"`
		Version     uint64   `json:"version"`
		ThresholdM  int      `json:"threshold_m"`
		ApproverIDs []string `json:"approver_ids"`
	}{"budget-policy:v1", p.ID, p.OwnerID, p.Version, p.ThresholdM, p.ApproverIDs})
}

type TaxStatus string

const (
	TaxStatusSettled    TaxStatus = "SETTLED"
	TaxStatusDebtExists TaxStatus = "DEBT_EXISTS"
	TaxStatusUnknown    TaxStatus = "UNKNOWN"
)

// TaxStatusSnapshot is the only data allowed to cross from TaxPrivate to
// BudgetPrivate: status, validity period, and a commitment. Tax returns,
// revenue, payment amounts, and tax case numbers are deliberately excluded.
type TaxStatusSnapshot struct {
	OrganizationID        string    `json:"organization_id"`
	Status                TaxStatus `json:"status"`
	ValidUntil            time.Time `json:"valid_until"`
	AttestationCommitment string    `json:"attestation_commitment"`
}

// BudgetRequestInput is signed by the requesting unit before it reaches the
// primary node. All of its fields belong exclusively to BudgetPrivate.
type BudgetRequestInput struct {
	OrganizationID  string `json:"organization_id"`
	ClientReference string `json:"client_reference"`
	BudgetProgram   string `json:"budget_program"`
	FiscalYear      uint32 `json:"fiscal_year"`
	Amount          uint64 `json:"amount"`
	Purpose         string `json:"purpose"`
	PolicyID        string `json:"policy_id"`
}

func (in BudgetRequestInput) SigningBytes() []byte {
	return canonicalJSON(struct {
		Domain string `json:"domain"`
		BudgetRequestInput
	}{"budget-request:v1", in})
}

// ApprovalSigningBytes binds an approval signature to one exact request and
// policy version, preventing replay for a different request or version.
func ApprovalSigningBytes(requestID, requestCommitment string, policyVersion uint64) []byte {
	return canonicalJSON(struct {
		Domain            string `json:"domain"`
		RequestID         string `json:"request_id"`
		RequestCommitment string `json:"request_commitment"`
		PolicyVersion     uint64 `json:"policy_version"`
	}{"budget-approval:v1", requestID, requestCommitment, policyVersion})
}

type Approval struct {
	Signature
	SignedAt time.Time `json:"signed_at"`
}

// BudgetRequest is the complete, confidential state of a request in
// BudgetPrivate. PublicReference and DisclosureNonce are retained only to build
// a hard-to-guess public event; they do not transfer financial data publicly.
type BudgetRequest struct {
	ID                 string             `json:"id"`
	Input              BudgetRequestInput `json:"input"`
	RequesterSignature Signature          `json:"requester_signature"`
	RequestCommitment  string             `json:"request_commitment"`
	Policy             ApprovalPolicy     `json:"policy"`
	TaxStatus          TaxStatusSnapshot  `json:"tax_status"`
	Approvals          []Approval         `json:"approvals"`
	Status             string             `json:"status"`
	SubmittedAt        time.Time          `json:"submitted_at"`
	ReleasedAt         *time.Time         `json:"released_at,omitempty"`
	CreditTransferID   string             `json:"credit_transfer_id,omitempty"`
	PublicReference    string             `json:"public_reference"`
	DisclosureNonce    string             `json:"disclosure_nonce"`
}

// CreditTransfer is created in the same state transition as the threshold
// approval. It represents budget release or transfer in the PoC, not a real
// banking-system integration.
type CreditTransfer struct {
	ID          string    `json:"id"`
	RequestID   string    `json:"request_id"`
	RecipientID string    `json:"recipient_id"`
	Amount      uint64    `json:"amount"`
	ExecutedAt  time.Time `json:"executed_at"`
	Reference   string    `json:"reference"`
}

// PublicBudgetRecord is all that BudgetPublic sees. It excludes amount, purpose,
// organizational identity, policy, and signatures. DisclosureCommitment uses a
// private nonce so it cannot be recreated from guessable request values.
type PublicBudgetRecord struct {
	PublicReference      string    `json:"public_reference"`
	Status               string    `json:"status"`
	ReleasedAt           time.Time `json:"released_at"`
	PublishedAt          time.Time `json:"published_at"`
	DisclosureCommitment string    `json:"disclosure_commitment"`
}

// TaxStatusInput is signed with the tax authority's private key in the
// independent TaxPrivate network. InternalCaseReference is never copied into a
// budget request or public ledger.
type TaxStatusInput struct {
	IssuerID              string    `json:"issuer_id"`
	OrganizationID        string    `json:"organization_id"`
	Status                TaxStatus `json:"status"`
	ValidUntil            time.Time `json:"valid_until"`
	InternalCaseReference string    `json:"internal_case_reference"`
}

func (in TaxStatusInput) SigningBytes() []byte {
	return canonicalJSON(struct {
		Domain string `json:"domain"`
		TaxStatusInput
	}{"tax-status:v1", in})
}

// TaxStatusAttestation is the complete signed attestation in TaxPrivate.
// Commitment supports private communication with the budget core; PublicReference
// and the nonce are used only when creating the minimal TaxPublic version.
type TaxStatusAttestation struct {
	ID              string         `json:"id"`
	Input           TaxStatusInput `json:"input"`
	Signature       Signature      `json:"signature"`
	IssuedAt        time.Time      `json:"issued_at"`
	Commitment      string         `json:"commitment"`
	PublicReference string         `json:"public_reference"`
	DisclosureNonce string         `json:"disclosure_nonce"`
}

// PublicTaxRecord is the controlled TaxPublic disclosure. It reveals neither the
// organizational identity nor case number, and its commitment is hard to guess
// because it incorporates a private nonce.
type PublicTaxRecord struct {
	PublicReference string    `json:"public_reference"`
	Status          TaxStatus `json:"status"`
	ValidUntil      time.Time `json:"valid_until"`
	Commitment      string    `json:"commitment"`
	PublishedAt     time.Time `json:"published_at"`
}

func canonicalJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
