package migrations

import "hijarr/internal/maintenance"

var registry = &maintenance.Registry{}

// Wire registers all known maintenance tasks.
func Wire(privKeyHex string) {
	// One-shot migrations (CategoryProtocol)
	registry.Register(newSRNResignV2(privKeyHex))
	
	// Community maintenance tasks (CategoryCleanup, etc.)
	// registry.Register(newSRNCleanupTask())
}

// GlobalRegistry returns the process-wide maintenance Registry.
func GlobalRegistry() *maintenance.Registry {
	return registry
}
