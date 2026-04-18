# Disposable Migration Framework

Hijarr uses a lightweight, one-shot migration system for data transformations that must run exactly once per installation—such as re-signing stored events after a protocol algorithm change, or backfilling a newly introduced field.

Migrations are **not** scheduler jobs. They run at process startup, block until complete, and restart the process. A migration that fails causes an immediate `os.Exit(1)` so the operator is alerted before normal operation resumes.

---

## Startup lifecycle

```
main() starts
  └─ migrations.Wire(privKeyHex)          ← register all known migrations
  └─ migration.NewRunner(store, registry)
  └─ runner.RunPending(ctx)
        ├─ no pending migrations → return nil, continue normal startup
        └─ first pending migration M found
             ├─ M.Run(ctx) succeeds
             │    └─ MarkApplied("M")
             │    └─ syscall.Exec (process replaces itself)
             │         → next boot: RunPending finds the next one, or nothing
             └─ M.Run(ctx) fails
                  └─ print error + os.Exit(1)  ← wait for operator
```

Key properties:
- **One migration per boot.** Each restart is a clean slate; in-memory state never leaks between migrations.
- **Ordered.** Migrations run in `Wire()` registration order.
- **Idempotent by contract.** `Run()` must be safe to re-execute if it was interrupted before `MarkApplied` completed.
- **Hard stop on failure.** A failing migration blocks startup entirely. Fix the root cause, then restart.

---

## Writing a new migration

### 1. Create `internal/migrations/my_migration.go`

```go
package migrations

import (
    "context"
    "fmt"
)

type myMigration struct {
    // inject any runtime dependencies here (private key, config paths, etc.)
}

func newMyMigration() *myMigration {
    return &myMigration{}
}

// ID must be globally unique and never change after the first deployment.
// Recommended format: "<scope>-<description>-v<N>", e.g. "srn-resign-v2".
func (m *myMigration) ID() string { return "scope-description-v1" }

// Run performs the migration. It must be idempotent: if it is interrupted
// partway through and retried, it must not corrupt data or double-apply changes.
func (m *myMigration) Run(ctx context.Context) error {
    fmt.Printf("🔧 [migration] scope-description-v1: starting\n")

    // ... your migration logic ...

    fmt.Printf("🔧 [migration] scope-description-v1: done\n")
    return nil
}
```

### 2. Register it in `Wire()` inside `internal/migrations/migrations.go`

```go
func Wire(privKeyHex string) {
    registry.Register(newSRNResignV2(privKeyHex)) // existing
    registry.Register(newMyMigration())           // ← append new ones at the end
}
```

**Always append.** Never reorder or remove an entry—that would change which migrations are considered "pending" for already-upgraded installs.

### 3. Verify

```bash
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test ./internal/migration/... -v
```

On the next startup, `RunPending` will pick up the new migration, run it, mark it done, and restart. Subsequent startups skip it.

---

## Runtime dependencies

`Wire(privKeyHex string)` is the injection point for anything migrations need at runtime. The string is the node's Ed25519 private key in hex form, read from the state store at startup:

```go
// cmd/hijarr/main.go (already wired)
privKeyHex := stateStore.GetIdentity("srn_priv_key")
migrations.Wire(privKeyHex)
```

If a future migration needs different dependencies, extend `Wire`'s signature or add a separate `WireX(...)` function—do not use package-level globals set outside of `Wire`.

---

## Existing migrations

| ID | File | Trigger condition |
| :--- | :--- | :--- |
| `srn-resign-v2` | `internal/migrations/srn_resign_v2.go` | SRN EventID algorithm upgraded from SHA256[:16] (32 hex) to full SHA256 (64 hex). Re-signs all `srn_queue` entries owned by this node and notifies the relay via `KindReplace (1003)`. |

---

## When to use (and when not to)

**Use a migration when:**
- A one-time data transformation is needed (re-signing, ID format change, schema backfill).
- The transformation must complete before normal operation can safely resume.
- The operation is too large or risky to handle inline at query time.

**Do not use a migration when:**
- The change is a regular SQL `ALTER TABLE`—handle that with `db.Exec("ALTER TABLE … ADD COLUMN …")` in the store's `newStore()` init block (see existing pattern in `srn/store.go`).
- The operation should run on every startup—use a scheduler `Job` instead.
- The transformation can be done lazily at read/write time with no correctness risk.

---

## Implementation notes

- **Store**: `applied_migrations` table in `StateDBPath` (same SQLite file as the rest of hijarr state). Schema: `id TEXT PRIMARY KEY, applied_at INTEGER, run_count INTEGER`.
- **Restart mechanism**: `os.Executable()` + `syscall.Exec`—replaces the current process image in-place, preserving PID lineage and open file descriptors are re-opened by the new process.
- **Duplicate ID guard**: `Registry.Register` panics at startup if the same ID is registered twice, catching mistakes at init time rather than at runtime.
