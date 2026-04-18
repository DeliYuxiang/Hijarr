package cache

import (
	"database/sql"
	"encoding/json"
	"sync"

	hijarrdb "hijarr/internal/db"
	"hijarr/internal/logger"
)

type TMDBCache struct {
	mem sync.Map
	db  *sql.DB
}

var (
	globalTMDBCacheOnce sync.Once
	globalTMDBCache     *TMDBCache
)

func InitTMDBCache(dbPath string) {
	globalTMDBCacheOnce.Do(func() {
		globalTMDBCache = &TMDBCache{}
		db, err := hijarrdb.Open(dbPath)
		if err != nil {
			logger.Error("⚠️  [TMDB API缓存] 无法打开 SQLite (%s): %v\n", dbPath, err)
			return
		}

		_, err = db.Exec(`CREATE TABLE IF NOT EXISTS tmdb_cache (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at INTEGER NOT NULL DEFAULT (unixepoch())
		)`)
		if err != nil {
			logger.Error("⚠️  [TMDB API缓存] 建表失败: %v\n", err)
			db.Close()
			return
		}
		globalTMDBCache.db = db
	})
}

// Get fetches from L1 mem cache, falling back to L2 DB cache.
func Get[T any](key string) (T, bool) {
	if globalTMDBCache == nil {
		var zero T
		return zero, false
	}

	// 1. Check L1 Memory Cache
	if v, ok := globalTMDBCache.mem.Load(key); ok {
		return v.(T), true
	}

	// 2. Check L2 Database
	if globalTMDBCache.db != nil {
		var raw string
		err := globalTMDBCache.db.QueryRow(`SELECT value FROM tmdb_cache WHERE key = ?`, key).Scan(&raw)
		if err == sql.ErrNoRows {
			var zero T
			return zero, false
		} else if err == nil {
			var val T
			if err := json.Unmarshal([]byte(raw), &val); err == nil {
				// Promote to L1 Memory
				globalTMDBCache.mem.Store(key, val)
				return val, true
			}
		}
	}

	var zero T
	return zero, false
}

func Set[T any](key string, value T) {
	if globalTMDBCache == nil {
		return
	}

	// 1. Update L1 Memory Cache
	globalTMDBCache.mem.Store(key, value)

	// 2. Persist to L2 Database
	if globalTMDBCache.db != nil {
		payload, err := json.Marshal(value)
		if err == nil {
			_, err := globalTMDBCache.db.Exec(`
				INSERT INTO tmdb_cache (key, value, updated_at) 
				VALUES (?, ?, unixepoch())
				ON CONFLICT(key) DO UPDATE SET 
					value = excluded.value, 
					updated_at = unixepoch()
			`, key, string(payload))
			if err != nil {
				logger.Warn("⚠️  [TMDB API缓存] 写入 SQLite 失败: %v\n", err)
			}
		}
	}
}
