package state

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	hijarrdb "hijarr/internal/db"
)

// Store manages Hijarr's specific state tables like seen files, failed files, etc.
// Reuses the same SQLite file as the subtitle cache (CACHE_DB_PATH).
type Store struct {
	db *sql.DB
}

var (
	globalStoreOnce sync.Once
	globalStore     *Store
)

// GetStore returns the process-wide state Store singleton.
func GetStore(dbPath string) *Store {
	globalStoreOnce.Do(func() {
		globalStore = newStore(dbPath)
	})
	return globalStore
}

func newStore(dbPath string) *Store {
	db, err := hijarrdb.Open(dbPath)
	if err != nil {
		fmt.Printf("⚠️  [State Store] 无法打开 SQLite: %v\n", err)
		return &Store{}
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS seen_files (
			path       TEXT PRIMARY KEY,
			mtime_ns   INTEGER NOT NULL
		);
		CREATE TABLE IF NOT EXISTS failed_files (
			path      TEXT PRIMARY KEY,
			failed_at INTEGER NOT NULL
		);
		CREATE TABLE IF NOT EXISTS global_state (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS subtitle_selections (
			video_path  TEXT PRIMARY KEY,
			archive_md5 TEXT NOT NULL,
			sub_md5     TEXT NOT NULL,
			selected_at INTEGER NOT NULL
		);
		CREATE TABLE IF NOT EXISTS subtitle_blacklist (
			event_hash TEXT PRIMARY KEY,
			cache_key  TEXT NOT NULL DEFAULT '',
			reason     TEXT NOT NULL DEFAULT '',
			blocked_at INTEGER NOT NULL
		);
		CREATE TABLE IF NOT EXISTS subtitle_pins (
			cache_key TEXT PRIMARY KEY,
			event_id  TEXT NOT NULL,
			pinned_at INTEGER NOT NULL
		);
	`)

	if err != nil {
		fmt.Printf("⚠️  [State Store] 建表失败: %v\n", err)
		db.Close()
		return &Store{}
	}

	return &Store{db: db}
}

// DB exposes the underlying *sql.DB if transaction across packages is needed.
func (s *Store) DB() *sql.DB {
	return s.db
}

// SeenMtime returns the previously recorded mtime (nanoseconds) for a file path, or 0 if unknown.
func (s *Store) SeenMtime(path string) int64 {
	if s.db == nil {
		return 0
	}
	var ns int64
	s.db.QueryRow(`SELECT mtime_ns FROM seen_files WHERE path = ?`, path).Scan(&ns)
	return ns
}

// SetSeenMtime records the mtime for a file path so the next scan can skip it if unchanged.
func (s *Store) SetSeenMtime(path string, mtime time.Time) {
	if s.db == nil {
		return
	}
	s.db.Exec(`INSERT INTO seen_files (path, mtime_ns) VALUES (?, ?)
		ON CONFLICT(path) DO UPDATE SET mtime_ns = excluded.mtime_ns`,
		path, mtime.UnixNano())
}

// SeenFileResult is one row from the seen_files table.
type SeenFileResult struct {
	Path    string `json:"path"`
	MtimeNS int64  `json:"mtime_ns"`
}

// ListSeenFiles returns all rows from seen_files. Optional pathFilter is a
// substring match against the path column (empty = no filter).
func (s *Store) ListSeenFiles(pathFilter string) ([]SeenFileResult, error) {
	if s.db == nil {
		return nil, nil
	}
	q := `SELECT path, mtime_ns FROM seen_files`
	args := []interface{}{}
	if pathFilter != "" {
		q += ` WHERE path LIKE ?`
		args = append(args, "%"+pathFilter+"%")
	}
	q += ` ORDER BY path`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SeenFileResult
	for rows.Next() {
		var r SeenFileResult
		if rows.Scan(&r.Path, &r.MtimeNS) == nil {
			out = append(out, r)
		}
	}
	return out, nil
}

// ClearAllSeenFiles removes every row from seen_files so DiskScan re-processes all files.
func (s *Store) ClearAllSeenFiles() (int64, error) {
	if s.db == nil {
		return 0, fmt.Errorf("store not initialized")
	}
	res, err := s.db.Exec(`DELETE FROM seen_files`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// DeleteSeenFile removes a single row from seen_files by path.
func (s *Store) DeleteSeenFile(path string) error {
	if s.db == nil {
		return fmt.Errorf("store not initialized")
	}
	_, err := s.db.Exec(`DELETE FROM seen_files WHERE path = ?`, path)
	return err
}

// SetFailedFile records that processing path failed, so future scans can skip it within TTL.
func (s *Store) SetFailedFile(path string) {
	if s.db == nil {
		return
	}
	s.db.Exec(`INSERT INTO failed_files (path, failed_at) VALUES (?, ?)
		ON CONFLICT(path) DO UPDATE SET failed_at = excluded.failed_at`,
		path, time.Now().Unix())
}

// IsFailedFile returns true if path failed processing within ttl duration.
// Returns false if never failed, or if the failure record is older than ttl.
func (s *Store) IsFailedFile(path string, ttl time.Duration) bool {
	if s.db == nil || ttl <= 0 {
		return false
	}
	var failedAt int64
	err := s.db.QueryRow(`SELECT failed_at FROM failed_files WHERE path = ?`, path).Scan(&failedAt)
	if err != nil {
		return false
	}
	return time.Since(time.Unix(failedAt, 0)) < ttl
}

// ClearAllFailedFiles removes every row from failed_files so DiskScan retries them all.
func (s *Store) ClearAllFailedFiles() (int64, error) {
	if s.db == nil {
		return 0, fmt.Errorf("store not initialized")
	}
	res, err := s.db.Exec(`DELETE FROM failed_files`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ClearFailedFile removes a path from the failed_files cache (e.g., after manual re-ingest).
func (s *Store) ClearFailedFile(path string) {
	if s.db == nil {
		return
	}
	s.db.Exec(`DELETE FROM failed_files WHERE path = ?`, path)
}

// FailedFileResult is one row from the failed_files table.
type FailedFileResult struct {
	Path     string `json:"path"`
	FailedAt int64  `json:"failed_at"`
}

// ListFailedFiles returns all rows from failed_files.
func (s *Store) ListFailedFiles() ([]FailedFileResult, error) {
	if s.db == nil {
		return nil, nil
	}
	q := `SELECT path, failed_at FROM failed_files ORDER BY failed_at DESC`
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FailedFileResult
	for rows.Next() {
		var r FailedFileResult
		if rows.Scan(&r.Path, &r.FailedAt) == nil {
			out = append(out, r)
		}
	}
	return out, nil
}

// DBStats holds row counts for all tables in the shared SQLite database.
type DBStats struct {
	SRNEvents int // srn_events: total subtitle entries
	SRNTitles int // srn_events: distinct titles
	SeenFiles int // seen_files: disk-scan mtime records
}

// FullStats queries all tables in the shared SQLite database and returns counts.
func (s *Store) FullStats() DBStats {
	if s.db == nil {
		return DBStats{}
	}
	var st DBStats
	s.db.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT title) FROM srn_events`).
		Scan(&st.SRNEvents, &st.SRNTitles)
	s.db.QueryRow(`SELECT COUNT(*) FROM seen_files`).Scan(&st.SeenFiles)
	return st
}

// GetIdentity returns a stored string value from global_state (e.g. private key).
func (s *Store) GetIdentity(key string) string {
	if s.db == nil {
		return ""
	}
	var v string
	s.db.QueryRow(`SELECT value FROM global_state WHERE key = ?`, key).Scan(&v)
	return v
}

// SetIdentity stores or updates a string value in global_state.
func (s *Store) SetIdentity(key, value string) {
	if s.db == nil {
		return
	}
	s.db.Exec(`INSERT INTO global_state (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
}

// SubtitleSelection records which specific subtitle version from SRN was chosen for a video.
type SubtitleSelection struct {
	ArchiveMD5 string `json:"archive_md5"`
	SubMD5     string `json:"sub_md5"`
}

// GetSubtitleSelection returns the recorded MD5s for a video path.
func (s *Store) GetSubtitleSelection(videoPath string) *SubtitleSelection {
	if s.db == nil {
		return nil
	}
	var sel SubtitleSelection
	err := s.db.QueryRow(`SELECT archive_md5, sub_md5 FROM subtitle_selections WHERE video_path = ?`, videoPath).Scan(&sel.ArchiveMD5, &sel.SubMD5)
	if err != nil {
		return nil
	}
	return &sel
}

// SetSubtitleSelection records the user's choice of subtitle for a video path.
func (s *Store) SetSubtitleSelection(videoPath, archiveMD5, subMD5 string) {
	if s.db == nil {
		return
	}
	s.db.Exec(`INSERT INTO subtitle_selections (video_path, archive_md5, sub_md5, selected_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(video_path) DO UPDATE SET archive_md5=excluded.archive_md5, sub_md5=excluded.sub_md5, selected_at=excluded.selected_at`,
		videoPath, archiveMD5, subMD5, time.Now().Unix())
}

// ── Subtitle Blacklist ────────────────────────────────────────────────────────

// BlacklistEntry is one row from the subtitle_blacklist table.
type BlacklistEntry struct {
	EventHash string `json:"event_hash"`
	CacheKey  string `json:"cache_key"`
	Reason    string `json:"reason"`
	BlockedAt int64  `json:"blocked_at"`
}

// ListBlacklist returns all blacklisted event hashes.
func (s *Store) ListBlacklist() ([]BlacklistEntry, error) {
	if s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT event_hash, cache_key, reason, blocked_at FROM subtitle_blacklist ORDER BY blocked_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BlacklistEntry
	for rows.Next() {
		var e BlacklistEntry
		if rows.Scan(&e.EventHash, &e.CacheKey, &e.Reason, &e.BlockedAt) == nil {
			out = append(out, e)
		}
	}
	return out, nil
}

// AddBlacklist adds an event hash to the blacklist.
func (s *Store) AddBlacklist(eventHash, cacheKey, reason string) error {
	if s.db == nil {
		return fmt.Errorf("store not initialized")
	}
	_, err := s.db.Exec(
		`INSERT INTO subtitle_blacklist (event_hash, cache_key, reason, blocked_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(event_hash) DO UPDATE SET cache_key=excluded.cache_key, reason=excluded.reason, blocked_at=excluded.blocked_at`,
		eventHash, cacheKey, reason, time.Now().Unix())
	return err
}

// RemoveBlacklist removes an event hash from the blacklist.
func (s *Store) RemoveBlacklist(eventHash string) error {
	if s.db == nil {
		return fmt.Errorf("store not initialized")
	}
	_, err := s.db.Exec(`DELETE FROM subtitle_blacklist WHERE event_hash = ?`, eventHash)
	return err
}

// IsBlacklisted returns true if the event hash is in the blacklist.
func (s *Store) IsBlacklisted(eventHash string) bool {
	if s.db == nil {
		return false
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM subtitle_blacklist WHERE event_hash = ?`, eventHash).Scan(&n)
	return n > 0
}

// ── Subtitle Pins ─────────────────────────────────────────────────────────────

// PinEntry is one row from the subtitle_pins table.
type PinEntry struct {
	CacheKey string `json:"cache_key"`
	EventID  string `json:"event_id"`
	PinnedAt int64  `json:"pinned_at"`
}

// ListPins returns all pinned cache keys.
func (s *Store) ListPins() ([]PinEntry, error) {
	if s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT cache_key, event_id, pinned_at FROM subtitle_pins ORDER BY pinned_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PinEntry
	for rows.Next() {
		var p PinEntry
		if rows.Scan(&p.CacheKey, &p.EventID, &p.PinnedAt) == nil {
			out = append(out, p)
		}
	}
	return out, nil
}

// SetPin pins a specific SRN event for a cache key.
func (s *Store) SetPin(cacheKey, eventID string) error {
	if s.db == nil {
		return fmt.Errorf("store not initialized")
	}
	_, err := s.db.Exec(
		`INSERT INTO subtitle_pins (cache_key, event_id, pinned_at) VALUES (?, ?, ?)
		ON CONFLICT(cache_key) DO UPDATE SET event_id=excluded.event_id, pinned_at=excluded.pinned_at`,
		cacheKey, eventID, time.Now().Unix())
	return err
}

// RemovePin removes the pin for a cache key.
func (s *Store) RemovePin(cacheKey string) error {
	if s.db == nil {
		return fmt.Errorf("store not initialized")
	}
	_, err := s.db.Exec(`DELETE FROM subtitle_pins WHERE cache_key = ?`, cacheKey)
	return err
}

// GetPin returns the pinned event ID for a cache key, or "" if not pinned.
func (s *Store) GetPin(cacheKey string) string {
	if s.db == nil {
		return ""
	}
	var id string
	s.db.QueryRow(`SELECT event_id FROM subtitle_pins WHERE cache_key = ?`, cacheKey).Scan(&id)
	return id
}
