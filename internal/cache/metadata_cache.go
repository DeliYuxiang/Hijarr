package cache

import (
	"database/sql"
	"encoding/json"
	"sync"

	hijarrdb "hijarr/internal/db"
	"hijarr/internal/logger"
)

var log = logger.For("cache")


// Metadata represents the parsed and verified info for a raw title.
type Metadata struct {
	RawTitle string   `json:"raw_title"`
	TMDBID   int      `json:"tmdb_id"`
	Title    string   `json:"title"`
	Season   int      `json:"season"`
	Episode  int      `json:"episode"`
	Aliases  []string `json:"aliases"`
}

type MetadataCache struct {
	mem sync.Map
	db  *sql.DB
}

var (
	globalMetadataCacheOnce sync.Once
	globalMetadataCache     *MetadataCache
)

func GetMetadataCache(dbPath string) *MetadataCache {
	globalMetadataCacheOnce.Do(func() {
		globalMetadataCache = newMetadataCache(dbPath)
	})
	return globalMetadataCache
}

func newMetadataCache(dbPath string) *MetadataCache {
	db, err := hijarrdb.Open(dbPath)
	if err != nil {
		log.Error("⚠️  [元数据缓存] 无法打开 SQLite (%s): %v\n", dbPath, err)
		return &MetadataCache{}
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS metadata_cache (
		raw_title  TEXT PRIMARY KEY,
		tmdb_id    INTEGER NOT NULL,
		title      TEXT NOT NULL,
		season     INTEGER NOT NULL,
		episode    INTEGER NOT NULL,
		aliases    TEXT NOT NULL,
		created_at INTEGER NOT NULL DEFAULT (unixepoch()),
		updated_at INTEGER NOT NULL DEFAULT (unixepoch())
	)`)
	if err != nil {
		log.Error("⚠️  [元数据缓存] 建表失败: %v\n", err)
		db.Close()
		return &MetadataCache{}
	}

	return &MetadataCache{db: db}
}

func (c *MetadataCache) Get(rawTitle string) (*Metadata, bool) {
	if v, ok := c.mem.Load(rawTitle); ok {
		return v.(*Metadata), true
	}

	if c.db == nil {
		return nil, false
	}

	var m Metadata
	var aliasesRaw string
	err := c.db.QueryRow(`
		SELECT tmdb_id, title, season, episode, aliases 
		FROM metadata_cache WHERE raw_title = ?`, rawTitle).Scan(
		&m.TMDBID, &m.Title, &m.Season, &m.Episode, &aliasesRaw)

	if err != nil {
		return nil, false
	}

	json.Unmarshal([]byte(aliasesRaw), &m.Aliases)
	m.RawTitle = rawTitle

	c.mem.Store(rawTitle, &m)
	return &m, true
}

// Invalidate removes a single metadata entry from both L1 and L2 by rawTitle.
func (c *MetadataCache) Invalidate(rawTitle string) error {
	c.mem.Delete(rawTitle)
	if c.db == nil {
		return nil
	}
	_, err := c.db.Exec(`DELETE FROM metadata_cache WHERE raw_title = ?`, rawTitle)
	return err
}

func (c *MetadataCache) Set(m *Metadata) {
	c.mem.Store(m.RawTitle, m)

	if c.db == nil {
		return
	}

	aliasesJSON, _ := json.Marshal(m.Aliases)

	_, err := c.db.Exec(`
		INSERT INTO metadata_cache (raw_title, tmdb_id, title, season, episode, aliases, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, unixepoch(), unixepoch())
		ON CONFLICT(raw_title) DO UPDATE SET
			tmdb_id    = excluded.tmdb_id,
			title      = excluded.title,
			season     = excluded.season,
			episode    = excluded.episode,
			aliases    = excluded.aliases,
			updated_at = unixepoch()
	`, m.RawTitle, m.TMDBID, m.Title, m.Season, m.Episode, string(aliasesJSON))

	if err != nil {
		log.Warn("⚠️  [元数据缓存] 写入 SQLite 失败: %v\n", err)
	}
}
