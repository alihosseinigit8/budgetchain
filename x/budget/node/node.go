// Package node composes the project's four ledgers in the primary node. It is
// the only permitted location for controlled data flow from TaxPrivate to
// BudgetPrivate and from private to public ledgers.
package node

import (
	"bytes"
	"fmt"
	"sync"
	"time"

	"budgetchain/x/budget/chain"
	"budgetchain/x/budget/contract"
	"budgetchain/x/budget/keeper"
	"budgetchain/x/budget/types"
	taxkeeper "budgetchain/x/tax/keeper"
)

// Node is the complete primary node of the PoC: it executes the contract,
// verifies organizational signatures through the registry, and sends only
// minimal records to public ledgers. It deliberately does not merge all four
// ledgers into one shared Chain.
type Node struct {
	Keeper    *keeper.Keeper
	TaxKeeper *taxkeeper.Keeper
	Contract  *contract.Contract

	BudgetPrivate *chain.Chain
	BudgetPublic  *chain.Chain
	TaxPrivate    *chain.Chain
	TaxPublic     *chain.Chain

	// Chain exists only for temporary compatibility with legacy demo calls and
	// points exactly to BudgetPrivate. New code should use each explicit ledger.
	Chain *chain.Chain

	mu                  sync.RWMutex
	persistMu           sync.Mutex
	storagePath         string
	budgetPublicRecords []types.PublicBudgetRecord
	taxPublicRecords    []types.PublicTaxRecord
	simulatedValidators map[string]*SimulatedValidator
}

type ValidatorObservation struct {
	Network       string
	Height        uint64
	BlockHash     string
	Valid         bool
	FailureReason string
	ObservedAt    time.Time
}

type validatorHead struct {
	Height uint64
	Hash   []byte
}

// SimulatedValidator models the minimum behavior of a non-primary validator.
// Before recording an observation, it checks height continuity, the previous
// hash, transaction root, and block hash. This is not PBFT, HotStuff, or real
// consensus: PoC validators neither vote nor form a quorum or re-execute state.
type SimulatedValidator struct {
	ID           string
	Online       bool
	Observations []ValidatorObservation
	heads        map[string]validatorHead
}

// New creates all four ledgers with independent genesis blocks. Independent
// genesis blocks matter because a tax StateRoot must not link to the budget
// chain, and vice versa.
func New() *Node {
	budgetState := keeper.NewKeeper()
	taxState := taxkeeper.NewKeeper()
	budgetPrivate := chain.New(budgetState.StateRoot())
	return &Node{
		Keeper:              budgetState,
		TaxKeeper:           taxState,
		Contract:            contract.New(budgetState),
		BudgetPrivate:       budgetPrivate,
		BudgetPublic:        chain.New(types.JSONDigest([]types.PublicBudgetRecord{})),
		TaxPrivate:          chain.New(taxState.StateRoot()),
		TaxPublic:           chain.New(types.JSONDigest([]types.PublicTaxRecord{})),
		Chain:               budgetPrivate,
		budgetPublicRecords: []types.PublicBudgetRecord{},
		taxPublicRecords:    []types.PublicTaxRecord{},
		simulatedValidators: make(map[string]*SimulatedValidator),
	}
}

// AddSimulatedValidator attaches a demonstration replica to the primary node.
// The replica starts from each ledger's current tip and structurally validates
// only subsequent blocks.
func (n *Node) AddSimulatedValidator(id string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if id == "" {
		return fmt.Errorf("validator id is required")
	}
	if _, exists := n.simulatedValidators[id]; exists {
		return fmt.Errorf("validator %q already exists", id)
	}
	n.simulatedValidators[id] = &SimulatedValidator{
		ID:           id,
		Online:       true,
		Observations: []ValidatorObservation{},
		heads: map[string]validatorHead{
			"budget-private": headOf(n.BudgetPrivate.Tip()),
			"budget-public":  headOf(n.BudgetPublic.Tip()),
			"tax-private":    headOf(n.TaxPrivate.Tip()),
			"tax-public":     headOf(n.TaxPublic.Tip()),
		},
	}
	return nil
}

// SetSimulatedValidatorOnline demonstrates a validator disconnect or reconnect.
// An offline validator does not stop the primary node because real consensus is
// not implemented here.
func (n *Node) SetSimulatedValidatorOnline(id string, online bool) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	validator, ok := n.simulatedValidators[id]
	if !ok {
		return fmt.Errorf("validator %q not found", id)
	}
	validator.Online = online
	return nil
}

// SimulatedValidatorSnapshot returns demo- or UI-safe output without granting
// write access to internal validator state.
func (n *Node) SimulatedValidatorSnapshot() []SimulatedValidator {
	n.mu.RLock()
	defer n.mu.RUnlock()
	result := make([]SimulatedValidator, 0, len(n.simulatedValidators))
	for _, validator := range n.simulatedValidators {
		copy := *validator
		copy.Observations = append([]ValidatorObservation(nil), validator.Observations...)
		result = append(result, copy)
	}
	return result
}

// RegisterBudgetIdentity records an organizational unit's public key in the
// BudgetPrivate identity registry and blocks that change. From this point on,
// SubmitBudgetRequest and ApproveBudgetRequest rely on the key's valid signature
// rather than a unit name alone.
func (n *Node) RegisterBudgetIdentity(identity types.Identity) (*types.Identity, error) {
	registered, err := n.Keeper.RegisterIdentity(identity)
	if err != nil {
		return nil, err
	}
	if _, err := n.commitBudgetPrivate(types.Tx{
		Type:    types.TxIdentityRegistration,
		Sender:  registered.ID,
		Payload: types.MustJSON(registered),
	}); err != nil {
		return nil, err
	}
	if err := n.persist(); err != nil {
		return nil, err
	}
	return registered, nil
}

// RegisterTaxAuthority belongs to the independent TaxPrivate network. Its key
// is accepted only for signing tax attestations, not for applying budget policy.
func (n *Node) RegisterTaxAuthority(identity types.Identity) (*types.Identity, error) {
	registered, err := n.TaxKeeper.RegisterAuthority(identity)
	if err != nil {
		return nil, err
	}
	if _, err := n.commitTaxPrivate(types.Tx{
		Type:    types.TxTaxAuthority,
		Sender:  registered.ID,
		Payload: types.MustJSON(registered),
	}); err != nil {
		return nil, err
	}
	if err := n.persist(); err != nil {
		return nil, err
	}
	return registered, nil
}

// UpsertApprovalPolicy is the controlled path for an m-of-n policy change. The
// policy administrator's signature is recorded both in the payload and private
// transaction signature.
func (n *Node) UpsertApprovalPolicy(actorID string, policy types.ApprovalPolicy, signature string) (*types.ApprovalPolicy, error) {
	result, err := n.Contract.Execute(contract.MsgUpsertPolicy{ActorID: actorID, Policy: policy, Signature: signature})
	if err != nil {
		return nil, err
	}
	if _, err := n.commitBudgetPrivate(types.Tx{
		Type:       types.TxPolicyUpdate,
		Sender:     actorID,
		Payload:    types.MustJSON(result.Policy),
		Signatures: []types.Signature{{SignerID: actorID, Value: signature}},
	}); err != nil {
		return nil, err
	}
	if err := n.persist(); err != nil {
		return nil, err
	}
	return result.Policy, nil
}

// SubmitTaxStatus stores a signed attestation only in TaxPrivate. Publication to
// TaxPublic is not automatic and must be explicitly requested with PublishTaxStatus.
func (n *Node) SubmitTaxStatus(input types.TaxStatusInput, signature string) (*types.TaxStatusAttestation, error) {
	attestation, err := n.TaxKeeper.SubmitStatus(input, signature)
	if err != nil {
		return nil, err
	}
	if _, err := n.commitTaxPrivate(types.Tx{
		Type:       types.TxTaxAttestation,
		Sender:     input.IssuerID,
		Payload:    types.MustJSON(attestation),
		Signatures: []types.Signature{attestation.Signature},
	}); err != nil {
		return nil, err
	}
	if err := n.persist(); err != nil {
		return nil, err
	}
	return attestation, nil
}

// PublishTaxStatus is the sole controlled gateway for tax-status publication.
// Its transaction payload contains no organizational identifier or case number
// and is built with a nonce-protected commitment.
func (n *Node) PublishTaxStatus(attestationID string) (*types.PublicTaxRecord, error) {
	record, err := n.TaxKeeper.BuildPublicDisclosure(attestationID)
	if err != nil {
		return nil, err
	}
	n.mu.Lock()
	n.taxPublicRecords = append(n.taxPublicRecords, *record)
	root := types.JSONDigest(n.taxPublicRecords)
	n.mu.Unlock()
	if _, err := n.commitTaxPublic(types.Tx{
		Type:    types.TxTaxDisclosure,
		Sender:  "primary-node",
		Payload: types.MustJSON(record),
	}, root); err != nil {
		return nil, err
	}
	if err := n.persist(); err != nil {
		return nil, err
	}
	return record, nil
}

// SubmitBudgetRequest first obtains a current tax snapshot, then the contract
// verifies the requester signature with the registry public key. The contract
// also rejects an existing organization-plus-client-reference key before this
// function reaches commitBudgetPrivate. Thus a duplicate returns ErrRequestExists
// without adding either a TxBudgetRequest or a block to BudgetPrivate. The index
// is rebuilt from committed history by Open, so the same rule survives restart.
// No public data is sent at this stage.
func (n *Node) SubmitBudgetRequest(input types.BudgetRequestInput, signature string) (*types.BudgetRequest, error) {
	taxStatus, err := n.TaxKeeper.StatusForBudget(input.OrganizationID, time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("tax-network status: %w", err)
	}
	result, err := n.Contract.Execute(contract.MsgSubmitBudgetRequest{Input: input, Signature: signature, TaxStatus: taxStatus})
	if err != nil {
		return nil, err
	}
	if _, err := n.commitBudgetPrivate(types.Tx{
		Type:       types.TxBudgetRequest,
		Sender:     input.OrganizationID,
		Payload:    types.MustJSON(result.Request),
		Signatures: []types.Signature{result.Request.RequesterSignature},
	}); err != nil {
		return nil, err
	}
	if err := n.persist(); err != nil {
		return nil, err
	}
	return result.Request, nil
}

// ApproveBudgetRequest records the approver signature in a private Tx. At the
// threshold approval, the contract creates a CreditTransfer in the same state
// transition, and the primary node publishes only a minimal pseudonymous release
// event to BudgetPublic.
func (n *Node) ApproveBudgetRequest(requestID, approverID, signature string) (*types.BudgetRequest, *types.CreditTransfer, error) {
	result, err := n.Contract.Execute(contract.MsgApproveBudgetRequest{RequestID: requestID, ApproverID: approverID, Signature: signature})
	if err != nil {
		return nil, nil, err
	}
	payload := struct {
		Request  *types.BudgetRequest  `json:"request"`
		Transfer *types.CreditTransfer `json:"transfer,omitempty"`
	}{result.Request, result.Transfer}
	if _, err := n.commitBudgetPrivate(types.Tx{
		Type:       types.TxBudgetApproval,
		Sender:     approverID,
		Payload:    types.MustJSON(payload),
		Signatures: []types.Signature{{SignerID: approverID, Value: signature}},
	}); err != nil {
		return nil, nil, err
	}
	if result.Transfer != nil {
		if _, err := n.publishBudgetRelease(result.Request.ID); err != nil {
			return nil, nil, err
		}
	}
	if err := n.persist(); err != nil {
		return nil, nil, err
	}
	return result.Request, result.Transfer, nil
}

// publishBudgetRelease is the internal path for budget-transparency publication.
// It is intentionally not a public API, preventing calls from exposing a PENDING
// request as a public event.
func (n *Node) publishBudgetRelease(requestID string) (*types.PublicBudgetRecord, error) {
	record, err := n.Keeper.BuildPublicDisclosure(requestID)
	if err != nil {
		return nil, err
	}
	n.mu.Lock()
	n.budgetPublicRecords = append(n.budgetPublicRecords, *record)
	root := types.JSONDigest(n.budgetPublicRecords)
	n.mu.Unlock()
	if _, err := n.commitBudgetPublic(types.Tx{
		Type:    types.TxBudgetDisclosure,
		Sender:  "primary-node",
		Payload: types.MustJSON(record),
	}, root); err != nil {
		return nil, err
	}
	return record, nil
}

// PublicBudgetRecords is the authorized output for the public transparency layer,
// not a complete view of BudgetPrivate requests.
func (n *Node) PublicBudgetRecords() []types.PublicBudgetRecord {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return append([]types.PublicBudgetRecord(nil), n.budgetPublicRecords...)
}

// PublicTaxRecords is the authorized output for controlled TaxPublic disclosure.
func (n *Node) PublicTaxRecords() []types.PublicTaxRecord {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return append([]types.PublicTaxRecord(nil), n.taxPublicRecords...)
}

// commitBudgetPrivate converts a transaction to a private block only after the
// contract succeeds, then notifies simulated validators for structural checks.
func (n *Node) commitBudgetPrivate(tx types.Tx) (*types.Block, error) {
	block, err := n.BudgetPrivate.Commit([]types.Tx{tx}, n.Keeper.StateRoot())
	if err == nil {
		n.observe("budget-private", block)
	}
	return block, err
}

// commitTaxPrivate applies the same pattern to the independent tax ledger.
func (n *Node) commitTaxPrivate(tx types.Tx) (*types.Block, error) {
	block, err := n.TaxPrivate.Commit([]types.Tx{tx}, n.TaxKeeper.StateRoot())
	if err == nil {
		n.observe("tax-private", block)
	}
	return block, err
}

// commitBudgetPublic writes only a sanitized public payload to the budget
// transparency ledger. Its root comes from public records, not the private Keeper.
func (n *Node) commitBudgetPublic(tx types.Tx, root []byte) (*types.Block, error) {
	block, err := n.BudgetPublic.Commit([]types.Tx{tx}, root)
	if err == nil {
		n.observe("budget-public", block)
	}
	return block, err
}

// commitTaxPublic records only the publishable version of a tax attestation.
func (n *Node) commitTaxPublic(tx types.Tx, root []byte) (*types.Block, error) {
	block, err := n.TaxPublic.Commit([]types.Tx{tx}, root)
	if err == nil {
		n.observe("tax-public", block)
	}
	return block, err
}

// observe executes the simulated validator behavior. Structural validation runs
// before an observation is recorded so the demo does more than store a height.
func (n *Node) observe(network string, block *types.Block) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, validator := range n.simulatedValidators {
		if validator.Online {
			validator.Observations = append(validator.Observations, validator.validateBlock(network, block))
		}
	}
}

func headOf(block *types.Block) validatorHead {
	return validatorHead{Height: block.Header.Height, Hash: append([]byte(nil), block.Hash...)}
}

// validateBlock performs the independent checks demonstrated by a PoC validator:
// block order, prev_hash, TxRoot, and header hash. Only after success does the
// validator's local tip advance to the new block.
func (v *SimulatedValidator) validateBlock(network string, block *types.Block) ValidatorObservation {
	observation := ValidatorObservation{
		Network: network, Height: block.Header.Height, BlockHash: block.HashHex(), ObservedAt: time.Now().UTC(),
	}
	head, ok := v.heads[network]
	if !ok {
		observation.FailureReason = "unknown network"
		return observation
	}
	if block.Header.Height != head.Height+1 {
		observation.FailureReason = "unexpected block height"
		return observation
	}
	if !bytes.Equal(block.Header.PrevHash, head.Hash) {
		observation.FailureReason = "previous hash does not match validator tip"
		return observation
	}
	if len(block.Txs) == 0 {
		observation.FailureReason = "empty non-genesis block"
		return observation
	}
	if !bytes.Equal(block.Header.TxRoot, types.TxRoot(block.Txs)) {
		observation.FailureReason = "transaction root mismatch"
		return observation
	}
	if !bytes.Equal(block.Hash, types.ComputeBlockHash(block.Header)) {
		observation.FailureReason = "block hash mismatch"
		return observation
	}
	v.heads[network] = headOf(block)
	observation.Valid = true
	return observation
}
