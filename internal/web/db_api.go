package web

import (
	"database/sql"
	"net/http"

	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"hijarr/internal/cache"
	"hijarr/internal/config"
	hijarrdb "hijarr/internal/db"
	"hijarr/internal/state"
)

// ── singleton DB connection ──────────────────────────────────────────

func getSRNDB() *sql.DB {
	db, _ := hijarrdb.Open(config.SRNDBPath)
	return db
}

func getStateDB() *sql.DB {
	return state.GetStore(config.StateDBPath).DB()
}

// ── route registration ──────────────────────────────────────────────────────

func registerDBAdminRoutes(g *gin.RouterGroup) {
	d := g.Group("/db")

	d.GET("/metadata-cache", handleMCList)
	d.POST("/metadata-cache/delete", handleMCDelete)
	d.POST("/metadata-cache/upsert", handleMCUpsert)

	d.GET("/srn-events", handleSEList)
	d.POST("/srn-events/delete", handleSEDelete)

	d.GET("/seen-files", handleSFList)
	d.POST("/seen-files/delete", handleSFDelete)

	d.GET("/failed-files", handleFFList)
	d.POST("/failed-files/delete", handleFFDelete)
}

// ── shared helpers ──────────────────────────────────────────────────────────

type pageResp struct {
	Rows  interface{} `json:"rows"`
	Total int         `json:"total"`
	Page  int         `json:"page"`
	Limit int         `json:"limit"`
}

type deleteReq struct {
	Keys []string `json:"keys"`
	All  bool     `json:"all"`
}

func paginate(c *gin.Context) (page, limit int, q string) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ = strconv.Atoi(c.DefaultQuery("limit", "50"))
	q = strings.TrimSpace(c.Query("q"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 500 {
		limit = 50
	}
	return
}

func likeWrap(s string) string { return "%" + s + "%" }

func dbErr(c *gin.Context, err error) {
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}

func countRows(db *sql.DB, table, where string, args ...interface{}) int {
	var n int
	q := "SELECT COUNT(*) FROM " + table
	if where != "" {
		q += " WHERE " + where
	}
	db.QueryRow(q, args...).Scan(&n) //nolint:errcheck
	return n
}

// ── srn_events ──────────────────────────────────────────────────────────────

type seRow struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Season    int    `json:"season"`
	Ep        int    `json:"ep"`
	Lang      string `json:"lang"`
	Filename  string `json:"filename"`
	SizeBytes int64  `json:"size_bytes"`
	CreatedAt int64  `json:"created_at"`
}

func handleSEList(c *gin.Context) {
	db := getSRNDB()
	if db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db unavailable"})
		return
	}
	page, limit, q := paginate(c)
	offset := (page - 1) * limit
	title := strings.TrimSpace(c.Query("title"))
	season, _ := strconv.Atoi(c.Query("season"))
	ep, _ := strconv.Atoi(c.Query("ep"))

	where := []string{}
	args := []interface{}{}
	if q != "" {
		where = append(where, "(title LIKE ? OR json_extract(event_json, '$.filename') LIKE ?)")
		lk := likeWrap(q)
		args = append(args, lk, lk)
	}
	if title != "" {
		where = append(where, "title LIKE ?")
		args = append(args, likeWrap(title))
	}
	if season > 0 {
		where = append(where, "season = ?")
		args = append(args, season)
	}
	if ep > 0 {
		where = append(where, "ep = ?")
		args = append(args, ep)
	}
	wClause := ""
	if len(where) > 0 {
		wClause = strings.Join(where, " AND ")
	}

	total := countRows(db, "srn_queue", wClause, args...)
	listArgs := append(args, limit, offset)
	baseQ := `SELECT id, title, season, ep, lang, json_extract(event_json, '$.filename'), length(content), created_at FROM srn_queue`
	if wClause != "" {
		baseQ += " WHERE " + wClause
	}
	baseQ += " ORDER BY created_at DESC LIMIT ? OFFSET ?"

	rs, err := db.Query(baseQ, listArgs...)
	if err != nil {
		dbErr(c, err)
		return
	}
	defer rs.Close()
	var rows []seRow
	for rs.Next() {
		var r seRow
		if rs.Scan(&r.ID, &r.Title, &r.Season, &r.Ep, &r.Lang, &r.Filename, &r.SizeBytes, &r.CreatedAt) == nil {
			rows = append(rows, r)
		}
	}
	if rows == nil {
		rows = []seRow{}
	}
	c.JSON(http.StatusOK, pageResp{Rows: rows, Total: total, Page: page, Limit: limit})
}

func handleSEDelete(c *gin.Context) {
	var req deleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	db := getSRNDB()
	if db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db unavailable"})
		return
	}
	if req.All {
		db.Exec(`DELETE FROM srn_queue`) //nolint:errcheck
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	for _, k := range req.Keys {
		db.Exec(`DELETE FROM srn_queue WHERE id = ?`, k) //nolint:errcheck
	}
	c.JSON(http.StatusOK, gin.H{"deleted": len(req.Keys)})
}

// ── seen_files ──────────────────────────────────────────────────────────────

type sfRow struct {
	Path    string `json:"path"`
	MtimeNS int64  `json:"mtime_ns"`
}

func handleSFList(c *gin.Context) {
	db := getStateDB()
	if db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db unavailable"})
		return
	}
	page, limit, q := paginate(c)
	offset := (page - 1) * limit
	var total int
	var rows []sfRow
	var rs *sql.Rows
	var err error
	if q != "" {
		lk := likeWrap(q)
		total = countRows(db, "seen_files", "path LIKE ?", lk)
		rs, err = db.Query(`SELECT path, mtime_ns FROM seen_files WHERE path LIKE ? ORDER BY path LIMIT ? OFFSET ?`, lk, limit, offset)
	} else {
		total = countRows(db, "seen_files", "")
		rs, err = db.Query(`SELECT path, mtime_ns FROM seen_files ORDER BY path LIMIT ? OFFSET ?`, limit, offset)
	}
	if err != nil {
		dbErr(c, err)
		return
	}
	defer rs.Close()
	for rs.Next() {
		var r sfRow
		if rs.Scan(&r.Path, &r.MtimeNS) == nil {
			rows = append(rows, r)
		}
	}
	if rows == nil {
		rows = []sfRow{}
	}
	c.JSON(http.StatusOK, pageResp{Rows: rows, Total: total, Page: page, Limit: limit})
}

func handleSFDelete(c *gin.Context) {
	var req deleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	db := getStateDB()
	if db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db unavailable"})
		return
	}
	if req.All {
		db.Exec(`DELETE FROM seen_files`) //nolint:errcheck
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	for _, k := range req.Keys {
		db.Exec(`DELETE FROM seen_files WHERE path = ?`, k) //nolint:errcheck
	}
	c.JSON(http.StatusOK, gin.H{"deleted": len(req.Keys)})
}

// ── failed_files ────────────────────────────────────────────────────────────

type ffRow struct {
	Path     string `json:"path"`
	FailedAt int64  `json:"failed_at"`
}

func handleFFList(c *gin.Context) {
	db := getStateDB()
	if db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db unavailable"})
		return
	}
	page, limit, q := paginate(c)
	offset := (page - 1) * limit
	var total int
	var rows []ffRow
	var rs *sql.Rows
	var err error
	if q != "" {
		lk := likeWrap(q)
		total = countRows(db, "failed_files", "path LIKE ?", lk)
		rs, err = db.Query(`SELECT path, failed_at FROM failed_files WHERE path LIKE ? ORDER BY failed_at DESC LIMIT ? OFFSET ?`, lk, limit, offset)
	} else {
		total = countRows(db, "failed_files", "")
		rs, err = db.Query(`SELECT path, failed_at FROM failed_files ORDER BY failed_at DESC LIMIT ? OFFSET ?`, limit, offset)
	}
	if err != nil {
		dbErr(c, err)
		return
	}
	defer rs.Close()
	for rs.Next() {
		var r ffRow
		if rs.Scan(&r.Path, &r.FailedAt) == nil {
			rows = append(rows, r)
		}
	}
	if rows == nil {
		rows = []ffRow{}
	}
	c.JSON(http.StatusOK, pageResp{Rows: rows, Total: total, Page: page, Limit: limit})
}

func handleFFDelete(c *gin.Context) {
	var req deleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	db := getStateDB()
	if db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db unavailable"})
		return
	}
	if req.All {
		db.Exec(`DELETE FROM failed_files`) //nolint:errcheck
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	for _, k := range req.Keys {
		db.Exec(`DELETE FROM failed_files WHERE path = ?`, k) //nolint:errcheck
	}
	c.JSON(http.StatusOK, gin.H{"deleted": len(req.Keys)})
}

// ── metadata_cache ───────────────────────────────────────────────────────────

func getCacheDB() *sql.DB {
	db, _ := hijarrdb.Open(config.CacheDBPath)
	return db
}

type mcRow struct {
	RawTitle  string `json:"raw_title"`
	TMDBID    int    `json:"tmdb_id"`
	Title     string `json:"title"`
	Season    int    `json:"season"`
	Episode   int    `json:"episode"`
	UpdatedAt int64  `json:"updated_at"`
}

func handleMCList(c *gin.Context) {
	db := getCacheDB()
	if db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db unavailable"})
		return
	}
	page, limit, q := paginate(c)
	offset := (page - 1) * limit
	var total int
	var rows []mcRow
	var rs *sql.Rows
	var err error
	if q != "" {
		lk := likeWrap(q)
		total = countRows(db, "metadata_cache", "raw_title LIKE ? OR title LIKE ?", lk, lk)
		rs, err = db.Query(`SELECT raw_title, tmdb_id, title, season, episode, updated_at FROM metadata_cache WHERE raw_title LIKE ? OR title LIKE ? ORDER BY updated_at DESC LIMIT ? OFFSET ?`, lk, lk, limit, offset)
	} else {
		total = countRows(db, "metadata_cache", "")
		rs, err = db.Query(`SELECT raw_title, tmdb_id, title, season, episode, updated_at FROM metadata_cache ORDER BY updated_at DESC LIMIT ? OFFSET ?`, limit, offset)
	}
	if err != nil {
		dbErr(c, err)
		return
	}
	defer rs.Close()
	for rs.Next() {
		var r mcRow
		if rs.Scan(&r.RawTitle, &r.TMDBID, &r.Title, &r.Season, &r.Episode, &r.UpdatedAt) == nil {
			rows = append(rows, r)
		}
	}
	if rows == nil {
		rows = []mcRow{}
	}
	c.JSON(http.StatusOK, pageResp{Rows: rows, Total: total, Page: page, Limit: limit})
}

func handleMCDelete(c *gin.Context) {
	var req deleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	mc := cache.GetMetadataCache(config.CacheDBPath)
	if req.All {
		db := getCacheDB()
		if db != nil {
			db.Exec(`DELETE FROM metadata_cache`) //nolint:errcheck
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	for _, k := range req.Keys {
		mc.Invalidate(k) //nolint:errcheck
	}
	c.JSON(http.StatusOK, gin.H{"deleted": len(req.Keys)})
}

func handleMCUpsert(c *gin.Context) {
	var body struct {
		RawTitle string   `json:"raw_title"`
		TMDBID   int      `json:"tmdb_id"`
		Title    string   `json:"title"`
		Season   int      `json:"season"`
		Episode  int      `json:"episode"`
		Aliases  []string `json:"aliases"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.RawTitle == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "raw_title required"})
		return
	}
	cache.GetMetadataCache(config.CacheDBPath).Set(&cache.Metadata{
		RawTitle: body.RawTitle,
		TMDBID:   body.TMDBID,
		Title:    body.Title,
		Season:   body.Season,
		Episode:  body.Episode,
		Aliases:  body.Aliases,
	})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ── /srn/api/events (used by SRNManagement.vue to browse local + remote nodes) ──

func handleSRNEvents(c *gin.Context) {
	db := getSRNDB()
	if db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db unavailable"})
		return
	}
	title := strings.TrimSpace(c.Query("title"))
	season, _ := strconv.Atoi(c.Query("season"))
	ep, _ := strconv.Atoi(c.Query("ep"))

	where := []string{}
	args := []interface{}{}
	if title != "" {
		where = append(where, "title LIKE ?")
		args = append(args, likeWrap(title))
	}
	if season > 0 {
		where = append(where, "season = ?")
		args = append(args, season)
	}
	if ep > 0 {
		where = append(where, "ep = ?")
		args = append(args, ep)
	}
	q := `SELECT id, title, season, ep, lang, json_extract(event_json, '$.filename'), length(content), created_at FROM srn_queue`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY created_at DESC LIMIT 500"

	rs, err := db.Query(q, args...)
	if err != nil {
		dbErr(c, err)
		return
	}
	defer rs.Close()
	var events []seRow
	for rs.Next() {
		var r seRow
		if rs.Scan(&r.ID, &r.Title, &r.Season, &r.Ep, &r.Lang, &r.Filename, &r.SizeBytes, &r.CreatedAt) == nil {
			events = append(events, r)
		}
	}
	if events == nil {
		events = []seRow{}
	}
	c.JSON(http.StatusOK, gin.H{"events": events})
}

func handleSRNEventDelete(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id required"})
		return
	}
	db := getSRNDB()
	if db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db unavailable"})
		return
	}
	db.Exec(`DELETE FROM srn_queue WHERE id = ?`, id) //nolint:errcheck
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
