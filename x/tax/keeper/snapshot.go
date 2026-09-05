package keeper

import (
	"fmt"

	"budgetchain/x/budget/types"
)

// Snapshot is the serializable private-tax state required to resume a node.
type Snapshot struct {
	Authorities  map[string]*types.Identity             `json:"authorities"`
	Attestations map[string]*types.TaxStatusAttestation `json:"attestations"`
	LatestByOrg  map[string]string                      `json:"latest_by_org"`
	Sequence     uint64                                 `json:"sequence"`
}

// Snapshot returns a detached serializable copy of private-tax state.
func (k *Keeper) Snapshot() Snapshot {
	k.mu.RLock()
	defer k.mu.RUnlock()

	snapshot := Snapshot{
		Authorities:  make(map[string]*types.Identity, len(k.authorities)),
		Attestations: make(map[string]*types.TaxStatusAttestation, len(k.attestations)),
		LatestByOrg:  make(map[string]string, len(k.latestByOrg)),
		Sequence:     k.sequence,
	}
	for id, authority := range k.authorities {
		snapshot.Authorities[id] = cloneIdentity(authority)
	}
	for id, attestation := range k.attestations {
		snapshot.Attestations[id] = cloneAttestation(attestation)
	}
	for organizationID, attestationID := range k.latestByOrg {
		snapshot.LatestByOrg[organizationID] = attestationID
	}
	return snapshot
}

// NewKeeperFromSnapshot restores private-tax state from durable storage.
func NewKeeperFromSnapshot(snapshot Snapshot) (*Keeper, error) {
	k := NewKeeper()
	k.sequence = snapshot.Sequence
	for id, authority := range snapshot.Authorities {
		if authority == nil || id == "" || authority.ID != id {
			return nil, fmt.Errorf("invalid persisted tax authority %q", id)
		}
		k.authorities[id] = cloneIdentity(authority)
	}
	for id, attestation := range snapshot.Attestations {
		if attestation == nil || id == "" || attestation.ID != id {
			return nil, fmt.Errorf("invalid persisted tax attestation %q", id)
		}
		k.attestations[id] = cloneAttestation(attestation)
	}
	for organizationID, attestationID := range snapshot.LatestByOrg {
		if organizationID == "" || k.attestations[attestationID] == nil {
			return nil, fmt.Errorf("invalid persisted latest tax status for %q", organizationID)
		}
		k.latestByOrg[organizationID] = attestationID
	}
	return k, nil
}
