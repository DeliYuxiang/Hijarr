package srn

import (
	"crypto/ed25519"

	"hijarr/internal/config"
	srnclient "github.com/DeliYuxiang/SRNApiClient/srn"
)

// Type aliases — srn.Event, srn.ErrPermanentUpload, and srn.RelayIdentity are
// identical to the shared module types; no conversion is needed at call sites.
type (
	Event             = srnclient.Event
	ErrPermanentUpload = srnclient.ErrPermanentUpload
	RelayIdentity     = srnclient.RelayIdentity
)

// SetNodeKey registers the node's Ed25519 identity key for signing outbound requests.
func SetNodeKey(priv ed25519.PrivateKey) { srnclient.SetNodeKey(priv) }

// queryOne queries a single relay — used by provider.go for BackendSRN lookups.
func queryOne(relayURL, tmdbID, lang string, season, ep int) ([]Event, error) {
	return srnclient.QueryRelay(relayURL, tmdbID, lang, season, ep)
}

// mergeEvents appends events from source into target, deduplicating by ID.
func mergeEvents(target *[]Event, source []Event) { srnclient.MergeEvents(target, source) }

// QueryNetworkForLangs queries all configured relays for each language, deduplicating results.
func QueryNetworkForLangs(tmdbID string, langs []config.SubtitleLanguage, season, ep int) []Event {
	strs := make([]string, len(langs))
	for i, l := range langs {
		strs[i] = string(l)
	}
	return srnclient.QueryNetworkForLangs(config.SRNRelayURLs, tmdbID, strs, season, ep)
}

// DownloadFromRelays fetches subtitle content by event ID from the configured relays.
func DownloadFromRelays(id string) ([]byte, error) {
	return srnclient.DownloadFromRelays(config.SRNRelayURLs, id)
}

// PublishToNetwork signs and broadcasts an event to all configured relays.
func PublishToNetwork(ev *Event, data []byte, privKey ed25519.PrivateKey) error {
	return srnclient.PublishToNetwork(config.SRNRelayURLs, ev, data, privKey)
}

// RetractEvent publishes a Kind 1002 retraction to all configured relays.
func RetractEvent(targetID, reason string, privKey ed25519.PrivateKey) error {
	return srnclient.RetractEvent(config.SRNRelayURLs, targetID, reason, privKey)
}

// ReplaceEvent publishes a Kind 1003 replacement to all configured relays.
func ReplaceEvent(prevID string, tags [][]string, data []byte, filename string, privKey ed25519.PrivateKey) error {
	return srnclient.ReplaceEvent(config.SRNRelayURLs, prevID, tags, data, filename, privKey)
}

// QueryRelayIdentity fetches the identity of a relay.
func QueryRelayIdentity(relayURL string) (*RelayIdentity, error) {
	return srnclient.QueryRelayIdentity(relayURL)
}
