package node

import (
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	budgetkeeper "budgetchain/x/budget/keeper"
	"budgetchain/x/budget/types"
)

// TestFourNetworkSignedBudgetFlow covers the end-to-end PoC path: identity and
// policy registration, tax attestation, signed request and approvals, controlled
// public disclosure, private auditor access, and structural ledger validation.
func TestFourNetworkSignedBudgetFlow(t *testing.T) {
	t.Log("creating the signed four-ledger budget flow")
	n := New()
	// Demonstration validators independently observe each committed ledger block.
	for _, validatorID := range []string{"validator-a", "validator-b"} {
		if err := n.AddSimulatedValidator(validatorID); err != nil {
			t.Fatal(err)
		}
	}

	// Register all budget-network roles with separate signing keys.
	keys := map[string]*ecdsa.PrivateKey{}
	for _, identity := range []struct {
		id    string
		roles []types.Role
	}{
		{"org-roads", []types.Role{types.RoleRequester}},
		{"policy-office", []types.Role{types.RolePolicyAdmin}},
		{"approver-a", []types.Role{types.RoleApprover}},
		{"approver-b", []types.Role{types.RoleApprover}},
		{"approver-c", []types.Role{types.RoleApprover}},
		{"internal-audit", []types.Role{types.RoleAuditor}},
	} {
		keys[identity.id] = mustKey(t)
		publicKey, err := types.EncodePublicKey(&keys[identity.id].PublicKey)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := n.RegisterBudgetIdentity(types.Identity{
			ID: identity.id, DisplayName: identity.id, PublicKey: publicKey, Roles: identity.roles, Active: true,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// The tax authority has a separate registry and key from budget roles.
	keys["tax-office"] = mustKey(t)
	taxPublicKey, err := types.EncodePublicKey(&keys["tax-office"].PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n.RegisterTaxAuthority(types.Identity{
		ID: "tax-office", DisplayName: "Tax office", PublicKey: taxPublicKey, Roles: []types.Role{types.RoleTaxAuthority}, Active: true,
	}); err != nil {
		t.Fatal(err)
	}

	// Configure the initial two-of-three approval policy.
	policy := types.ApprovalPolicy{
		ID: "capital-spend", OwnerID: "policy-office", Version: 1, ThresholdM: 2,
		ApproverIDs: []string{"approver-a", "approver-b", "approver-c"},
	}
	policySignature := mustSign(t, keys["policy-office"], policy.SigningBytes())
	if _, err := n.UpsertApprovalPolicy("policy-office", policy, policySignature); err != nil {
		t.Fatal(err)
	}

	// A valid, signed tax attestation is required before budget submission.
	taxInput := types.TaxStatusInput{
		IssuerID: "tax-office", OrganizationID: "org-roads", Status: types.TaxStatusSettled,
		ValidUntil: time.Now().UTC().Add(24 * time.Hour), InternalCaseReference: "confidential-case-884",
	}
	taxSignature := mustSign(t, keys["tax-office"], taxInput.SigningBytes())
	tax, err := n.SubmitTaxStatus(taxInput, taxSignature)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n.PublishTaxStatus(tax.ID); err != nil {
		t.Fatal(err)
	}

	// A request signed by a different role's private key must be rejected.
	requestInput := types.BudgetRequestInput{
		OrganizationID: "org-roads", ClientReference: "REQ-2026-17", BudgetProgram: "road-maintenance",
		FiscalYear: 1405, Amount: 1_200_000_000, Purpose: "emergency bridge repair", PolicyID: policy.ID,
	}
	forgedInput := requestInput
	forgedInput.ClientReference = "REQ-2026-FORGED"
	if _, err := n.SubmitBudgetRequest(forgedInput, mustSign(t, keys["approver-a"], forgedInput.SigningBytes())); err == nil {
		t.Fatal("a request signed by a different organization's private key must be rejected")
	}
	requestSignature := mustSign(t, keys["org-roads"], requestInput.SigningBytes())
	request, err := n.SubmitBudgetRequest(requestInput, requestSignature)
	if err != nil {
		t.Fatal(err)
	}
	if request.Status != types.StatusPending || len(request.Approvals) != 0 {
		t.Fatalf("new request must be pending: %#v", request)
	}
	if len(n.BudgetPrivate.Tip().Txs[0].Signatures) != 1 {
		t.Fatal("the signed request must carry its signature in the private block")
	}

	// Updating the policy after submission must not alter this request snapshot.
	// Even after a new one-of-three policy, this request remains two-of-three.
	updatedPolicy := types.ApprovalPolicy{
		ID: policy.ID, OwnerID: policy.OwnerID, Version: 2, ThresholdM: 1,
		ApproverIDs: append([]string(nil), policy.ApproverIDs...),
	}
	updatedSignature := mustSign(t, keys["policy-office"], updatedPolicy.SigningBytes())
	if _, err := n.UpsertApprovalPolicy("policy-office", updatedPolicy, updatedSignature); err != nil {
		t.Fatal(err)
	}

	// Approval signatures bind the request commitment to its captured policy version.
	approvalBytes := types.ApprovalSigningBytes(request.ID, request.RequestCommitment, request.Policy.Version)
	firstSignature := mustSign(t, keys["approver-a"], approvalBytes)
	request, transfer, err := n.ApproveBudgetRequest(request.ID, "approver-a", firstSignature)
	if err != nil {
		t.Fatal(err)
	}
	if transfer != nil || request.Status != types.StatusPending || len(n.PublicBudgetRecords()) != 0 {
		t.Fatal("one approval must not release a request submitted under 2-of-3")
	}

	// The second valid approval meets the original threshold and releases credit.
	secondSignature := mustSign(t, keys["approver-b"], approvalBytes)
	request, transfer, err = n.ApproveBudgetRequest(request.ID, "approver-b", secondSignature)
	if err != nil {
		t.Fatal(err)
	}
	if transfer == nil || request.Status != types.StatusReleased || transfer.Amount != requestInput.Amount {
		t.Fatalf("threshold approval must release the credit: request=%#v transfer=%#v", request, transfer)
	}
	if len(n.BudgetPrivate.Tip().Txs[0].Signatures) != 1 {
		t.Fatal("the approval signature must be committed in the private block")
	}

	// BudgetPublic must contain only the controlled disclosure, never private data.
	publicBudget := n.PublicBudgetRecords()
	if len(publicBudget) != 1 || publicBudget[0].PublicReference != request.PublicReference || publicBudget[0].DisclosureCommitment == "" {
		t.Fatalf("expected one public budget commitment, got %#v", publicBudget)
	}
	publicJSON, err := json.Marshal(publicBudget[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"org-roads", "emergency bridge repair", "1200000000", "approver-a", request.RequestCommitment, request.DisclosureNonce} {
		if strings.Contains(string(publicJSON), forbidden) {
			t.Fatalf("public budget record leaked %q: %s", forbidden, publicJSON)
		}
	}

	// TaxPublic must use a separate nonce-protected commitment and hide case data.
	publicTax := n.PublicTaxRecords()
	if len(publicTax) != 1 {
		t.Fatal("expected the explicitly published tax status")
	}
	if publicTax[0].Commitment == tax.Commitment {
		t.Fatal("public tax commitment must be separately salted from the private attestation commitment")
	}
	taxJSON, err := json.Marshal(publicTax[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"org-roads", "confidential-case-884", "tax-office", tax.Commitment, tax.DisclosureNonce} {
		if strings.Contains(string(taxJSON), forbidden) {
			t.Fatalf("public tax record leaked %q: %s", forbidden, taxJSON)
		}
	}

	// Full private request access is restricted to the auditor role.
	if _, err := n.Keeper.GetRequestForAuditor("internal-audit", request.ID); err != nil {
		t.Fatalf("authorized auditor should read private request: %v", err)
	}
	if _, err := n.Keeper.GetRequestForAuditor("org-roads", request.ID); err == nil {
		t.Fatal("requesting organization must not receive auditor access by default")
	}
	// Every independent ledger must retain a valid hash-linked structure.
	for name, ledger := range map[string]interface{ Verify() error }{
		"budget-private": n.BudgetPrivate,
		"budget-public":  n.BudgetPublic,
		"tax-private":    n.TaxPrivate,
		"tax-public":     n.TaxPublic,
	} {
		if err := ledger.Verify(); err != nil {
			t.Fatalf("%s chain invalid: %v", name, err)
		}
	}
	// Simulated validators must observe each ledger and report only valid blocks.
	for _, validator := range n.SimulatedValidatorSnapshot() {
		if len(validator.Observations) == 0 {
			t.Fatalf("%s did not observe any simulated replication", validator.ID)
		}
		for _, observation := range validator.Observations {
			if !observation.Valid {
				t.Fatalf("%s accepted an invalid observation: %#v", validator.ID, observation)
			}
		}
	}
	t.Log("verified signing, threshold release, controlled disclosure, and ledger integrity")
}

// TestPersistentHistoryRebuildsDuplicateRequestProtection proves that the
// duplicate-request guard survives a restart. The persisted reference index is
// intentionally rebuilt from committed BudgetPrivate request transactions.
func TestPersistentHistoryRebuildsDuplicateRequestProtection(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "budgetchain-state.json")
	n, err := Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("created a persistent node state file")

	keys := map[string]*ecdsa.PrivateKey{
		"requesting-unit": mustKey(t),
		"policy-office":   mustKey(t),
		"approver-a":      mustKey(t),
		"tax-office":      mustKey(t),
	}
	for _, identity := range []struct {
		id    string
		roles []types.Role
	}{
		{"requesting-unit", []types.Role{types.RoleRequester}},
		{"policy-office", []types.Role{types.RolePolicyAdmin}},
		{"approver-a", []types.Role{types.RoleApprover}},
	} {
		publicKey, err := types.EncodePublicKey(&keys[identity.id].PublicKey)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := n.RegisterBudgetIdentity(types.Identity{
			ID: identity.id, DisplayName: identity.id, PublicKey: publicKey, Roles: identity.roles, Active: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	taxPublicKey, err := types.EncodePublicKey(&keys["tax-office"].PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n.RegisterTaxAuthority(types.Identity{
		ID: "tax-office", DisplayName: "Tax office", PublicKey: taxPublicKey, Roles: []types.Role{types.RoleTaxAuthority}, Active: true,
	}); err != nil {
		t.Fatal(err)
	}

	policy := types.ApprovalPolicy{
		ID: "maintenance", OwnerID: "policy-office", Version: 1, ThresholdM: 1,
		ApproverIDs: []string{"approver-a"},
	}
	if _, err := n.UpsertApprovalPolicy("policy-office", policy, mustSign(t, keys["policy-office"], policy.SigningBytes())); err != nil {
		t.Fatal(err)
	}
	taxInput := types.TaxStatusInput{
		IssuerID: "tax-office", OrganizationID: "requesting-unit", Status: types.TaxStatusSettled,
		ValidUntil: time.Now().UTC().Add(24 * time.Hour), InternalCaseReference: "case-recovery-01",
	}
	if _, err := n.SubmitTaxStatus(taxInput, mustSign(t, keys["tax-office"], taxInput.SigningBytes())); err != nil {
		t.Fatal(err)
	}

	input := types.BudgetRequestInput{
		OrganizationID: "requesting-unit", ClientReference: "REQ-RECOVERY-01", BudgetProgram: "maintenance",
		FiscalYear: 1405, Amount: 150_000, Purpose: "replace damaged safety barrier", PolicyID: policy.ID,
	}
	if _, err := n.SubmitBudgetRequest(input, mustSign(t, keys["requesting-unit"], input.SigningBytes())); err != nil {
		t.Fatal(err)
	}
	heightBeforeRestart := n.BudgetPrivate.Height()
	if got := committedRequestCount(t, n, input); got != 1 {
		t.Fatalf("expected exactly one committed request before retry, got %d", got)
	}

	// The live guard is reached before Node.commitBudgetPrivate, so a retry must
	// neither create a transaction nor raise the private-ledger height.
	_, err = n.SubmitBudgetRequest(input, mustSign(t, keys["requesting-unit"], input.SigningBytes()))
	if !errors.Is(err, budgetkeeper.ErrRequestExists) {
		t.Fatalf("live node must reject a duplicate request, got %v", err)
	}
	if got := n.BudgetPrivate.Height(); got != heightBeforeRestart {
		t.Fatalf("duplicate request changed budget-private height: got %d want %d", got, heightBeforeRestart)
	}
	if got := committedRequestCount(t, n, input); got != 1 {
		t.Fatalf("duplicate request was added to history: found %d matching transactions", got)
	}
	t.Log("live duplicate request rejected before any BudgetPrivate transaction or block was created")

	// Reopening validates persisted histories and derives the duplicate index from
	// request transactions rather than from the previous process memory.
	restarted, err := Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.BudgetPrivate.Height() != heightBeforeRestart {
		t.Fatalf("budget-private height changed after restart: got %d want %d", restarted.BudgetPrivate.Height(), heightBeforeRestart)
	}
	if err := restarted.BudgetPrivate.Verify(); err != nil {
		t.Fatalf("restarted budget-private chain is invalid: %v", err)
	}
	heightBeforeHistoricalRetry := restarted.BudgetPrivate.Height()
	_, err = restarted.SubmitBudgetRequest(input, mustSign(t, keys["requesting-unit"], input.SigningBytes()))
	if !errors.Is(err, budgetkeeper.ErrRequestExists) {
		t.Fatalf("restarted node must reject the historical duplicate request, got %v", err)
	}
	if got := restarted.BudgetPrivate.Height(); got != heightBeforeHistoricalRetry {
		t.Fatalf("historical duplicate changed budget-private height: got %d want %d", got, heightBeforeHistoricalRetry)
	}
	if got := committedRequestCount(t, restarted, input); got != 1 {
		t.Fatalf("historical duplicate was added to history: found %d matching transactions", got)
	}
	t.Log("restarted node rebuilt the duplicate-request guard from BudgetPrivate history and added no new block")
}

// committedRequestCount reads the committed private-ledger history rather than
// Keeper's in-memory index, proving the test's claim about the actual chain.
func committedRequestCount(t *testing.T, n *Node, input types.BudgetRequestInput) int {
	t.Helper()
	count := 0
	for _, block := range n.BudgetPrivate.Blocks {
		for _, tx := range block.Txs {
			if tx.Type != types.TxBudgetRequest {
				continue
			}
			var request types.BudgetRequest
			if err := json.Unmarshal(tx.Payload, &request); err != nil {
				t.Fatalf("decode committed request transaction: %v", err)
			}
			if request.Input.OrganizationID == input.OrganizationID && request.Input.ClientReference == input.ClientReference {
				count++
			}
		}
	}
	return count
}

// TestSimulatedValidatorRejectsTamperedTransactionRoot proves that a simulated
// validator does more than store heights: it rejects a block with a changed
// transaction root even when its header hash has been recalculated.
func TestSimulatedValidatorRejectsTamperedTransactionRoot(t *testing.T) {
	n := New()
	validator := &SimulatedValidator{
		ID: "validator-test",
		heads: map[string]validatorHead{
			"budget-private": headOf(n.BudgetPrivate.Tip()),
		},
	}
	tip := n.BudgetPrivate.Tip()
	block := types.NewBlock(tip.Header.Height+1, tip.Hash, []byte("state-root"), []types.Tx{{
		Type: types.TxBudgetRequest, Sender: "org-test", Payload: []byte("signed-private-request"),
	}})
	block.Header.TxRoot = []byte("tampered-transaction-root")
	block.Hash = types.ComputeBlockHash(block.Header)

	observation := validator.validateBlock("budget-private", block)
	if observation.Valid || observation.FailureReason != "transaction root mismatch" {
		t.Fatalf("tampered block must be rejected, got %#v", observation)
	}
}

// mustKey creates a test-only private key or fails the current test immediately.
func mustKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := types.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// mustSign creates a test signature or fails the current test immediately.
func mustSign(t *testing.T, key *ecdsa.PrivateKey, payload []byte) string {
	t.Helper()
	signature, err := types.Sign(key, payload)
	if err != nil {
		t.Fatal(err)
	}
	return signature
}
