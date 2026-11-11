package discover

import "time"

// Snapshot is an immutable view of discovery state. Specs and APIStatus maps
// and their byte slices are copies owned by the caller.
//
// API keys are service hostnames, matching the keys returned by Specs. APIs
// contains every currently active service with an eligible HTTP port. Specs
// contains only APIs with a successfully processed specification, including
// retained last-good data for APIs whose latest attempt failed.
type Snapshot struct {
	Specs    map[string][]byte
	APIs     map[string]APIStatus
	Synced   bool
	Revision uint64
}

// APIStatus describes the latest discovery outcome for an active API.
// Failure is empty after a successful fetch and contains a sanitized category
// after failure. A zero LastSuccess means no valid specification has yet been
// discovered for this API. Generation identifies the active service instance
// and changes when a deleted hostname is added again.
type APIStatus struct {
	LastSuccess time.Time
	Failure     string
	Generation  uint64
}

// DiscoveryManager Default Object for discoverer manager.
type DiscoveryManager interface {
	// Shutdown safely stops Discovery process.
	Shutdown()
	// Run starts the discovery process.
	Run()
	// Specs returns the cached instances of discovered specs.
	Specs() map[string][]byte
	// RegisterOnChangeFunc provides a way to notifier a consumer of the Specs that data has changed instead of constantly checking
	RegisterOnChangeFunc(f func())
}

// SnapshotProvider is implemented by discovery managers that expose outcome-
// aware immutable snapshots without changing the DiscoveryManager contract.
type SnapshotProvider interface {
	Snapshot() Snapshot
}
