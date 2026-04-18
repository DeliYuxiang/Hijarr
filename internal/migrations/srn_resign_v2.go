package migrations

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"hijarr/internal/config"
	"hijarr/internal/maintenance"
	"hijarr/internal/srn"
)

type srnResignV2 struct {
	privKeyHex string
}

func newSRNResignV2(privKeyHex string) *srnResignV2 {
	return &srnResignV2{privKeyHex: privKeyHex}
}

func (m *srnResignV2) ID() string { return "srn-resign-v2" }

func (m *srnResignV2) Category() maintenance.TaskCategory {
	return maintenance.CategoryProtocol
}

// Run iterates all KindSubtitle (1001) events belonging to this node's pubkey,
// upgrades their IDs from the V1 truncated form (32 hex chars) to the V2 full
// SHA256 form (64 hex chars), re-signs them, and notifies the relay via
// KindReplace (1003) when the ID changes.
func (m *srnResignV2) Run(ctx context.Context) error {
	if m.privKeyHex == "" {
		return fmt.Errorf("srn-resign-v2: private key not configured (srn_priv_key missing from state)")
	}
	privBytes, err := hex.DecodeString(m.privKeyHex)
	if err != nil || len(privBytes) != ed25519.PrivateKeySize {
		return fmt.Errorf("srn-resign-v2: invalid private key hex")
	}
	privKey := ed25519.PrivateKey(privBytes)
	pubKeyHex := hex.EncodeToString(privKey.Public().(ed25519.PublicKey))

	store := srn.GetStore(config.SRNDBPath)

	var succeeded, skipped, failed int

	scanErr := store.ScanByPubKey(pubKeyHex, func(oldID string, ev *srn.Event, content []byte, _ string) bool {
		if ctx.Err() != nil {
			return false
		}
		newID := ev.ComputeIDV2()

		if newID == oldID {
			// ID is already V2; re-sign in place to refresh the signature.
			if err := ev.Sign(privKey); err != nil {
				fmt.Printf("🔧 [maintenance] srn-resign-v2: sign failed for %s: %v\n", oldID[:8], err)
				failed++
				return true
			}
			newJSON, _ := json.Marshal(ev)
			if uerr := store.UpdateEventJSON(oldID, string(newJSON)); uerr != nil {
				fmt.Printf("🔧 [maintenance] srn-resign-v2: update failed for %s: %v\n", oldID[:8], uerr)
				failed++
				return true
			}
			skipped++
			return true
		}

		// ID changed (V1 → V2): publish KindReplace to the relay, then update local queue.
		fmt.Printf("🔧 [maintenance] srn-resign-v2: %s → %s\n", oldID[:8], newID[:8])
		replErr := srn.ReplaceEvent(oldID, ev.Tags, content, ev.Filename, privKey)
		if replErr != nil {
			var pe *srn.ErrPermanentUpload
			if errors.As(replErr, &pe) && pe.StatusCode == 409 {
				// Relay already knows about this replacement — idempotent.
			} else {
				fmt.Printf("🔧 [maintenance] srn-resign-v2: ReplaceEvent failed %s: %v\n", oldID[:8], replErr)
				failed++
				return true
			}
		}

		ev.ID = newID
		if err := ev.Sign(privKey); err != nil {
			fmt.Printf("🔧 [maintenance] srn-resign-v2: sign v2 failed %s: %v\n", oldID[:8], err)
			failed++
			return true
		}
		newJSON, _ := json.Marshal(ev)
		if uerr := store.ReplaceQueueID(oldID, newID, string(newJSON)); uerr != nil {
			// Non-fatal: relay side succeeded; local queue will self-heal on next enqueue.
			fmt.Printf("🔧 [maintenance] srn-resign-v2: ReplaceQueueID failed %s: %v\n", oldID[:8], uerr)
		}
		succeeded++
		return true
	})

	fmt.Printf("🔧 [maintenance] srn-resign-v2 完成: 成功=%d 重签=%d 失败=%d\n", succeeded, skipped, failed)
	if scanErr != nil {
		return fmt.Errorf("srn-resign-v2: scan error: %w", scanErr)
	}
	if failed > 0 {
		return fmt.Errorf("srn-resign-v2: %d events failed", failed)
	}
	return nil
}
