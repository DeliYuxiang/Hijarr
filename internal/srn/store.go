package srn

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	hijarrdb "hijarr/internal/db"
)

// Store is the local SRN staging area (Queue) and cache.
// Reuses the same SQLite file as the subtitle cache (CACHE_DB_PATH).
type Store struct {
	db *sql.DB
}

// QueuedEvent represents an event waiting to be broadcasted to the network.
type QueuedEvent struct {
	ID        string `json:"id"`
	Event     *Event `json:"event"`
	Data      []byte `json:"-"`
	PrivKey   []byte `json:"-"`
	CreatedAt int64  `json:"created_at"`
	Attempts  int    `json:"attempts"`
	LastError string `json:"last_error,omitempty"`
}

var (
	globalStoreOnce sync.Once
	globalStore     *Store
)

// GetStore returns the process-wide SRN Store singleton.
func GetStore(dbPath string) *Store {
	globalStoreOnce.Do(func() {
		globalStore = newStore(dbPath)
	})
	return globalStore
}

func newStore(dbPath string) *Store {
	db, err := hijarrdb.Open(dbPath)
	if err != nil {
		log.Error("⚠️  [SRN Store] 无法打开 SQLite: %v\n", err)
		return &Store{}
	}

	// srn_queue: The primary L2 buffer for pending uploads.
	// We use generated columns to allow local Querying without duplicating data in separate columns.
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS srn_queue (
			id             TEXT PRIMARY KEY,
			event_json     TEXT NOT NULL,
			content        BLOB NOT NULL,
			priv_key       TEXT NOT NULL, -- hex encoded ed25519.PrivateKey
			created_at     INTEGER NOT NULL,
			attempts       INTEGER NOT NULL DEFAULT 0,
			last_error     TEXT NOT NULL DEFAULT '',
			next_retry_at  INTEGER NOT NULL DEFAULT 0, -- unix ts; 0 = immediately eligible
			-- Virtual columns for local lookup (assumes tags are stable-indexed or we use json_each in queries)
			-- Tags format: [["tmdb","123"],["title","Name"],["language","zh-CN"],["s","1"],["e","1"]]
			tmdb_id     TEXT GENERATED ALWAYS AS (json_extract(event_json, '$.tags[0][1]')) VIRTUAL,
			title       TEXT GENERATED ALWAYS AS (json_extract(event_json, '$.tags[1][1]')) VIRTUAL,
			lang        TEXT GENERATED ALWAYS AS (json_extract(event_json, '$.tags[2][1]')) VIRTUAL,
			season      INTEGER GENERATED ALWAYS AS (json_extract(event_json, '$.tags[3][1]')) VIRTUAL,
			ep          INTEGER GENERATED ALWAYS AS (json_extract(event_json, '$.tags[4][1]')) VIRTUAL
		);
		CREATE INDEX IF NOT EXISTS idx_queue_tmdb_s_e ON srn_queue (tmdb_id, season, ep);
		CREATE INDEX IF NOT EXISTS idx_queue_title_s_e ON srn_queue (title, season, ep);
		CREATE INDEX IF NOT EXISTS idx_queue_created  ON srn_queue (created_at);
	`)
	if err != nil {
		log.Error("⚠️  [SRN Store] 建表失败: %v\n", err)
		db.Close()
		return &Store{}
	}
	// Migration: add next_retry_at to existing tables that predate this column.
	// Silently ignored if the column already exists (new installs or already migrated).
	db.Exec(`ALTER TABLE srn_queue ADD COLUMN next_retry_at INTEGER NOT NULL DEFAULT 0`)

	log.Info("✅ [SRN Store] 已就绪 (Queue Mode): %s\n", dbPath)
	return &Store{db: db}
}

// Enqueue adds an event and its content to the local L2 buffer.
func (s *Store) Enqueue(ev *Event, data []byte, privKey []byte) error {
	if s.db == nil {
		return fmt.Errorf("store not initialized")
	}
	evJSON, _ := json.Marshal(ev)
	privHex := hex.EncodeToString(privKey)

	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO srn_queue (id, event_json, content, priv_key, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, ev.ID, string(evJSON), data, privHex, time.Now().Unix())
	return err
}

// GetTasks retrieves events that are pending upload and past their retry delay.
func (s *Store) GetTasks(limit int) ([]*QueuedEvent, error) {
	if s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query(`
		SELECT id, event_json, content, priv_key, created_at, attempts, last_error
		FROM srn_queue
		WHERE next_retry_at <= ?
		ORDER BY created_at ASC
		LIMIT ?
	`, time.Now().Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*QueuedEvent
	for rows.Next() {
		var q QueuedEvent
		var evJSON, privHex string
		if err := rows.Scan(&q.ID, &evJSON, &q.Data, &privHex, &q.CreatedAt, &q.Attempts, &q.LastError); err == nil {
			if json.Unmarshal([]byte(evJSON), &q.Event) == nil {
				q.PrivKey, _ = hex.DecodeString(privHex)
				tasks = append(tasks, &q)
			}
		}
	}
	return tasks, nil
}

// MarkFailed increments the attempt counter, records the error message, and sets a
// retry delay so the task is not immediately re-attempted on the next worker tick.
func (s *Store) MarkFailed(id string, lastErr string, retryAfter time.Duration) {
	if s.db == nil {
		return
	}
	s.db.Exec(
		`UPDATE srn_queue SET attempts = attempts + 1, last_error = ?, next_retry_at = ? WHERE id = ?`,
		lastErr, time.Now().Add(retryAfter).Unix(), id,
	)
}

// Remove deletes an event from the queue (usually after successful upload).
func (s *Store) Remove(id string) {
	if s.db == nil {
		return
	}
	s.db.Exec(`DELETE FROM srn_queue WHERE id = ?`, id)
}

// UpdateEventJSON replaces the stored event_json for an existing queue entry.
// Used by migration tasks to update a re-signed event without changing its ID.
func (s *Store) UpdateEventJSON(id, newJSON string) error {
	if s.db == nil {
		return fmt.Errorf("store not initialized")
	}
	_, err := s.db.Exec(`UPDATE srn_queue SET event_json = ? WHERE id = ?`, newJSON, id)
	return err
}

// ReplaceQueueID atomically renames a queue entry from oldID to newID and
// updates its serialised event_json.  Used by migration tasks after an ID
// algorithm upgrade (V1 truncated → V2 full SHA256).
func (s *Store) ReplaceQueueID(oldID, newID, newJSON string) error {
	if s.db == nil {
		return fmt.Errorf("store not initialized")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE srn_queue SET id = ?, event_json = ? WHERE id = ?`, newID, newJSON, oldID)
	if err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ScanByPubKey iterates all KindSubtitle (1001) events in srn_queue that belong
// to the given pubkey. fn receives the row data; return false to stop early.
// Used exclusively by migration tasks.
func (s *Store) ScanByPubKey(pubKeyHex string, fn func(id string, ev *Event, content []byte, privHex string) bool) error {
	if s.db == nil {
		return fmt.Errorf("store not initialized")
	}
	rows, err := s.db.Query(`
		SELECT id, event_json, content, priv_key
		FROM srn_queue
		WHERE json_extract(event_json, '$.pubkey') = ?
		  AND json_extract(event_json, '$.kind') = 1001
		ORDER BY created_at ASC
	`, pubKeyHex)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, evJSON, privHex string
		var content []byte
		if err := rows.Scan(&id, &evJSON, &content, &privHex); err != nil {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(evJSON), &ev); err != nil {
			continue
		}
		if !fn(id, &ev, content, privHex) {
			break
		}
	}
	return rows.Err()
}

// QueryResult is a row returned by Query.
type QueryResult struct {
	ID       string
	Filename string
	Title    string
	Season   int
	Ep       int
	Lang     string
}

// Query finds subtitles in the local queue that haven't been uploaded yet.
// Provides compatibility with search logic during offline periods.
func (s *Store) Query(tmdbID, title string, season, ep int) ([]QueryResult, error) {
	if s.db == nil {
		return nil, nil
	}

	var rows *sql.Rows
	var err error

	if tmdbID != "" {
		rows, err = s.db.Query(`
			SELECT id, json_extract(event_json, '$.filename'), title, season, ep, lang
			FROM srn_queue
			WHERE tmdb_id = ? AND season = ? AND ep = ?
			ORDER BY created_at DESC
		`, tmdbID, season, ep)
	} else {
		rows, err = s.db.Query(`
			SELECT id, json_extract(event_json, '$.filename'), title, season, ep, lang
			FROM srn_queue
			WHERE lower(title) LIKE lower(?) AND season = ? AND ep = ?
			ORDER BY created_at DESC
		`, "%"+strings.TrimSpace(title)+"%", season, ep)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []QueryResult
	for rows.Next() {
		var r QueryResult
		var filename sql.NullString
		if err := rows.Scan(&r.ID, &filename, &r.Title, &r.Season, &r.Ep, &r.Lang); err == nil {
			r.Filename = filename.String
			results = append(results, r)
		}
	}
	return results, nil
}

// QueryEvents finds subtitles in the local queue and returns full Event objects.
// lang supports prefix matching: "zh" matches "zh-hans", "zh-hant", etc.
func (s *Store) QueryEvents(tmdbID, title string, season, ep int, lang string) ([]Event, error) {
	if s.db == nil {
		return nil, nil
	}

	var rows *sql.Rows
	var err error

	// Build language filter: empty = all, prefix match for codes without "-", exact for codes with "-"
	langFilter := ""
	if lang != "" && !strings.Contains(lang, "-") {
		langFilter = lang // prefix match handled in SQL via LIKE
	}

	if tmdbID != "" {
		if langFilter != "" {
			rows, err = s.db.Query(`
				SELECT event_json FROM srn_queue
				WHERE tmdb_id = ? AND season = ? AND ep = ?
				  AND lang LIKE ? || '%'
				ORDER BY created_at DESC LIMIT 100
			`, tmdbID, season, ep, langFilter)
		} else if lang != "" {
			rows, err = s.db.Query(`
				SELECT event_json FROM srn_queue
				WHERE tmdb_id = ? AND season = ? AND ep = ? AND lang = ?
				ORDER BY created_at DESC LIMIT 100
			`, tmdbID, season, ep, lang)
		} else {
			rows, err = s.db.Query(`
				SELECT event_json FROM srn_queue
				WHERE tmdb_id = ? AND season = ? AND ep = ?
				ORDER BY created_at DESC LIMIT 100
			`, tmdbID, season, ep)
		}
	} else {
		if langFilter != "" {
			rows, err = s.db.Query(`
				SELECT event_json FROM srn_queue
				WHERE lower(title) LIKE lower(?) AND season = ? AND ep = ?
				  AND lang LIKE ? || '%'
				ORDER BY created_at DESC LIMIT 100
			`, "%"+strings.TrimSpace(title)+"%", season, ep, langFilter)
		} else if lang != "" {
			rows, err = s.db.Query(`
				SELECT event_json FROM srn_queue
				WHERE lower(title) LIKE lower(?) AND season = ? AND ep = ? AND lang = ?
				ORDER BY created_at DESC LIMIT 100
			`, "%"+strings.TrimSpace(title)+"%", season, ep, lang)
		} else {
			rows, err = s.db.Query(`
				SELECT event_json FROM srn_queue
				WHERE lower(title) LIKE lower(?) AND season = ? AND ep = ?
				ORDER BY created_at DESC LIMIT 100
			`, "%"+strings.TrimSpace(title)+"%", season, ep)
		}
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var evJSON string
		if err := rows.Scan(&evJSON); err != nil {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(evJSON), &ev); err != nil {
			continue
		}
		events = append(events, ev)
	}
	return events, rows.Err()
}

// GetContent returns the raw subtitle bytes and filename for a given event ID.
func (s *Store) GetContent(id string) ([]byte, string, error) {
	if s.db == nil {
		return nil, "", fmt.Errorf("store not initialized")
	}
	var data []byte
	var evJSON string
	err := s.db.QueryRow(`SELECT content, event_json FROM srn_queue WHERE id = ?`, id).Scan(&data, &evJSON)
	if err != nil {
		return nil, "", err
	}
	var ev Event
	json.Unmarshal([]byte(evJSON), &ev)
	return data, ev.Filename, nil
}

// Stats returns the number of items currently in the queue.
func (s *Store) Stats() (count int) {
	if s.db == nil {
		return 0
	}
	s.db.QueryRow(`SELECT COUNT(*) FROM srn_queue`).Scan(&count)
	return count
}

// BacklogStatus returns an empty string when immediately-eligible tasks are below threshold,
// or a human-readable status line when backlogged (eligible >= threshold).
// Eligible = next_retry_at <= now; deferred = still waiting out their retry delay.
func (s *Store) BacklogStatus(threshold int) string {
	if s.db == nil {
		return ""
	}
	now := time.Now().Unix()
	var eligible, total int
	s.db.QueryRow(`SELECT COUNT(*) FROM srn_queue WHERE next_retry_at <= ?`, now).Scan(&eligible)
	if eligible < threshold {
		return ""
	}
	s.db.QueryRow(`SELECT COUNT(*) FROM srn_queue`).Scan(&total)
	deferred := total - eligible
	if deferred > 0 {
		return fmt.Sprintf("队列积压 %d 条 (可调度 %d，等待重试 %d)", total, eligible, deferred)
	}
	return fmt.Sprintf("队列积压 %d 条", total)
}
