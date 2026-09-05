// Package keeper keeps private tax-network state independent of the budget
// core. This separation establishes the required boundary between confidential
// tax data and the budget-allocation process.
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
	ErrAuthorityNotFound   = errors.New("tax authority not found")
	ErrAuthorityExists     = errors.New("tax authority already registered")
	ErrInvalidAuthority    = errors.New("invalid tax authority")
	ErrInvalidStatus       = errors.New("invalid tax status attestation")
	ErrStatusNotFound      = errors.New("current tax status not found")
	ErrAttestationNotFound = errors.New("tax attestation not found")
)

// Keeper must not be merged with BudgetKeeper because its records include
// internal tax case references that must never enter the budget network or a
// public layer.
type Keeper struct {
	mu           sync.RWMutex
	authorities  map[string]*types.Identity
	attestations map[string]*types.TaxStatusAttestation
	latestByOrg  map[string]string
	sequence     uint64
}

func NewKeeper() *Keeper {
	return &Keeper{
		authorities:  make(map[string]*types.Identity),
		attestations: make(map[string]*types.TaxStatusAttestation),
		latestByOrg:  make(map[string]string),
	}
}

// RegisterAuthority adds an authorized tax authority's public key to the
// independent TaxPrivate registry. Keeping this registry separate prevents a tax
// authority from implicitly receiving permission to approve budgets.
func (k *Keeper) RegisterAuthority(identity types.Identity) (*types.Identity, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if strings.TrimSpace(identity.ID) == "" || strings.TrimSpace(identity.DisplayName) == "" || strings.TrimSpace(identity.PublicKey) == "" || !identity.Active || !identity.HasRole(types.RoleTaxAuthority) {
		return nil, ErrInvalidAuthority
	}
	if _, err := types.DecodePublicKey(identity.PublicKey); err != nil {
		return nil, err
	}
	if _, exists := k.authorities[identity.ID]; exists {
		return nil, ErrAuthorityExists
	}
	identity.Roles = append([]types.Role(nil), identity.Roles...)
	k.authorities[identity.ID] = &identity
	return cloneIdentity(&identity), nil
}

// SubmitStatus verifies the tax authority signature before recording an
// attestation. The case reference and complete signature stay in TaxPrivate;
// BudgetPrivate receives only a minimal snapshot.
func (k *Keeper) SubmitStatus(input types.TaxStatusInput, signature string) (*types.TaxStatusAttestation, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := validateStatus(input, signature); err != nil {
		return nil, err
	}
	authority, ok := k.authorities[input.IssuerID]
	if !ok {
		return nil, ErrAuthorityNotFound
	}
	if err := types.VerifySignature(authority.PublicKey, input.SigningBytes(), signature); err != nil {
		return nil, err
	}
	// The public reference and nonce must be random. Deriving them from a sequence
	// number or tax-data hash would increase public linkability to private data.
	publicReference, err := types.NewOpaqueToken("tax-public")
	if err != nil {
		return nil, err
	}
	disclosureNonce, err := types.NewOpaqueToken("tax-nonce")
	if err != nil {
		return nil, err
	}
	k.sequence++
	now := time.Now().UTC()
	attestation := &types.TaxStatusAttestation{
		ID:              fmt.Sprintf("tax-status-%06d", k.sequence),
		Input:           input,
		Signature:       types.Signature{SignerID: input.IssuerID, Value: signature},
		IssuedAt:        now,
		PublicReference: publicReference,
		DisclosureNonce: disclosureNonce,
	}
	attestation.Commitment = types.DigestHex(types.MustJSON(struct {
		Domain    string               `json:"domain"`
		ID        string               `json:"id"`
		Input     types.TaxStatusInput `json:"input"`
		Signature types.Signature      `json:"signature"`
	}{"tax-attestation:v2", attestation.ID, input, attestation.Signature}))
	k.attestations[attestation.ID] = attestation
	k.latestByOrg[input.OrganizationID] = attestation.ID
	return cloneAttestation(attestation), nil
}

// StatusForBudget provides only the minimum required information to the budget
// core. It never returns a complete TaxStatusAttestation or an InternalCaseReference.
func (k *Keeper) StatusForBudget(organizationID string, now time.Time) (types.TaxStatusSnapshot, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	attestationID, ok := k.latestByOrg[organizationID]
	if !ok {
		return types.TaxStatusSnapshot{}, ErrStatusNotFound
	}
	attestation := k.attestations[attestationID]
	if attestation == nil || !attestation.Input.ValidUntil.After(now.UTC()) {
		return types.TaxStatusSnapshot{}, ErrStatusNotFound
	}
	return types.TaxStatusSnapshot{
		OrganizationID:        attestation.Input.OrganizationID,
		Status:                attestation.Input.Status,
		ValidUntil:            attestation.Input.ValidUntil,
		AttestationCommitment: attestation.Commitment,
	}, nil
}

// BuildPublicDisclosure creates the deliberately limited TaxPublic record. Tax
// status is published through the primary node only when this function is called
// explicitly; the output excludes organizational identity and case references.
func (k *Keeper) BuildPublicDisclosure(attestationID string) (*types.PublicTaxRecord, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	attestation, ok := k.attestations[attestationID]
	if !ok {
		return nil, ErrAttestationNotFound
	}
	publicCommitment := types.DigestHex(types.MustJSON(struct {
		Domain          string          `json:"domain"`
		PublicReference string          `json:"public_reference"`
		Commitment      string          `json:"commitment"`
		DisclosureNonce string          `json:"disclosure_nonce"`
		Status          types.TaxStatus `json:"status"`
		ValidUntil      time.Time       `json:"valid_until"`
	}{"tax-public-event:v2", attestation.PublicReference, attestation.Commitment, attestation.DisclosureNonce, attestation.Input.Status, attestation.Input.ValidUntil}))
	return &types.PublicTaxRecord{
		PublicReference: attestation.PublicReference,
		Status:          attestation.Input.Status,
		ValidUntil:      attestation.Input.ValidUntil,
		Commitment:      publicCommitment,
		PublishedAt:     time.Now().UTC(),
	}, nil
}

// StateRoot links independent TaxPrivate state to that ledger's block headers.
// No part of this root is used as a substitute for the budget-network StateRoot.
func (k *Keeper) StateRoot() []byte {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return types.JSONDigest(struct {
		Authorities  map[string]*types.Identity             `json:"authorities"`
		Attestations map[string]*types.TaxStatusAttestation `json:"attestations"`
		LatestByOrg  map[string]string                      `json:"latest_by_org"`
		Sequence     uint64                                 `json:"sequence"`
	}{k.authorities, k.attestations, k.latestByOrg, k.sequence})
}

func validateStatus(input types.TaxStatusInput, signature string) error {
	if strings.TrimSpace(input.IssuerID) == "" || strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.InternalCaseReference) == "" || strings.TrimSpace(signature) == "" || input.ValidUntil.Before(time.Now().UTC()) {
		return ErrInvalidStatus
	}
	if input.Status != types.TaxStatusSettled && input.Status != types.TaxStatusDebtExists {
		return ErrInvalidStatus
	}
	return nil
}

func cloneIdentity(identity *types.Identity) *types.Identity {
	if identity == nil {
		return nil
	}
	copy := *identity
	copy.Roles = append([]types.Role(nil), identity.Roles...)
	return &copy
}

func cloneAttestation(attestation *types.TaxStatusAttestation) *types.TaxStatusAttestation {
	if attestation == nil {
		return nil
	}
	copy := *attestation
	return &copy
}
