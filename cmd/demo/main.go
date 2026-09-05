package main

import (
	"crypto/ecdsa"
	"fmt"
	"log"
	"time"

	"budgetchain/x/budget/node"
	"budgetchain/x/budget/types"
)

func main() {
	// The primary node creates all four ledgers; validators demonstrate simulated
	// replication and structural block validation.
	n := node.New()
	must(n.AddSimulatedValidator("validator-a"))
	must(n.AddSimulatedValidator("validator-b"))

	keys := map[string]*ecdsa.PrivateKey{}
	registerBudgetIdentity := func(id string, roles ...types.Role) {
		keys[id] = mustKey()
		publicKey, err := types.EncodePublicKey(&keys[id].PublicKey)
		must(err)
		_, err = n.RegisterBudgetIdentity(types.Identity{ID: id, DisplayName: id, PublicKey: publicKey, Roles: roles, Active: true})
		must(err)
	}
	// Organizational public keys enter the private budget registry. Private keys
	// remain only in this demo program to create request and approval signatures.
	registerBudgetIdentity("org-roads", types.RoleRequester)
	registerBudgetIdentity("policy-office", types.RolePolicyAdmin)
	registerBudgetIdentity("approver-a", types.RoleApprover)
	registerBudgetIdentity("approver-b", types.RoleApprover)
	registerBudgetIdentity("approver-c", types.RoleApprover)
	registerBudgetIdentity("internal-audit", types.RoleAuditor)

	keys["tax-office"] = mustKey()
	taxPublicKey, err := types.EncodePublicKey(&keys["tax-office"].PublicKey)
	must(err)
	_, err = n.RegisterTaxAuthority(types.Identity{
		ID: "tax-office", DisplayName: "Tax office", PublicKey: taxPublicKey, Roles: []types.Role{types.RoleTaxAuthority}, Active: true,
	})
	must(err)

	policy := types.ApprovalPolicy{
		ID: "capital-spend", OwnerID: "policy-office", Version: 1, ThresholdM: 2,
		ApproverIDs: []string{"approver-a", "approver-b", "approver-c"},
	}
	_, err = n.UpsertApprovalPolicy("policy-office", policy, sign(keys["policy-office"], policy.SigningBytes()))
	must(err)

	// The tax attestation is recorded separately in TaxPrivate before its controlled
	// version is published to TaxPublic. The internal case reference never enters
	// the budget ledger.
	taxInput := types.TaxStatusInput{
		IssuerID: "tax-office", OrganizationID: "org-roads", Status: types.TaxStatusSettled,
		ValidUntil: time.Now().UTC().Add(24 * time.Hour), InternalCaseReference: "confidential-case-884",
	}
	tax, err := n.SubmitTaxStatus(taxInput, sign(keys["tax-office"], taxInput.SigningBytes()))
	must(err)
	_, err = n.PublishTaxStatus(tax.ID)
	must(err)

	// The requesting unit signs this private data. The node verifies it using the
	// key registered in BudgetPrivate and sends nothing to BudgetPublic yet.
	requestInput := types.BudgetRequestInput{
		OrganizationID: "org-roads", ClientReference: "REQ-2026-17", BudgetProgram: "road-maintenance",
		FiscalYear: 1405, Amount: 1_200_000_000, Purpose: "emergency bridge repair", PolicyID: policy.ID,
	}
	request, err := n.SubmitBudgetRequest(requestInput, sign(keys["org-roads"], requestInput.SigningBytes()))
	must(err)
	fmt.Printf("Request %s submitted; status=%s, policy=%d-of-%d\n", request.ID, request.Status, request.Policy.ThresholdM, len(request.Policy.ApproverIDs))

	// Each approval is bound to this request commitment and policy version, so it
	// cannot be replayed for another request or policy.
	approvalPayload := types.ApprovalSigningBytes(request.ID, request.RequestCommitment, request.Policy.Version)
	request, transfer, err := n.ApproveBudgetRequest(request.ID, "approver-a", sign(keys["approver-a"], approvalPayload))
	must(err)
	fmt.Printf("First approval: status=%s, transfer=%v\n", request.Status, transfer != nil)
	request, transfer, err = n.ApproveBudgetRequest(request.ID, "approver-b", sign(keys["approver-b"], approvalPayload))
	must(err)
	fmt.Printf("Second approval: status=%s, credit transfer=%s\n", request.Status, transfer.ID)

	for name, ledger := range map[string]interface {
		Height() uint64
		Verify() error
	}{
		"budget-private": n.BudgetPrivate,
		"budget-public":  n.BudgetPublic,
		"tax-private":    n.TaxPrivate,
		"tax-public":     n.TaxPublic,
	} {
		must(ledger.Verify())
		fmt.Printf("%s: height=%d\n", name, ledger.Height())
	}
	fmt.Printf("public budget records=%d; public tax records=%d\n", len(n.PublicBudgetRecords()), len(n.PublicTaxRecords()))
}

func mustKey() *ecdsa.PrivateKey {
	key, err := types.NewPrivateKey()
	must(err)
	return key
}

func sign(key *ecdsa.PrivateKey, payload []byte) string {
	signature, err := types.Sign(key, payload)
	must(err)
	return signature
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
