package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"hijarr/internal/config"
	"hijarr/internal/metrics"
	"hijarr/internal/state"
	"hijarr/internal/tmdb"

	"github.com/gin-gonic/gin"
)

//go:embed frontend_dist/index.html
var frontendIndex []byte

//go:embed frontend_dist
var frontendFS embed.FS

var startTime = time.Now()

var registeredJobs []map[string]string

// SetJobsInfo stores the list of registered scheduler jobs for the /jobs endpoint.
func SetJobsInfo(jobs []map[string]string) { registeredJobs = jobs }

var nodePublicKey string

// SetNodePublicKey stores the node's Ed25519 public key (hex) for the /config endpoint.
func SetNodePublicKey(pubHex string) { nodePublicKey = pubHex }

func RegisterRoutes(r *gin.Engine) {
	// ── Serve Vue frontend (SPA) ──────────────────────────────────────────
	assetsSub, _ := fs.Sub(frontendFS, "frontend_dist/assets")
	r.StaticFS("/assets", http.FS(assetsSub))

	r.GET("/favicon.svg", func(c *gin.Context) {
		data, _ := frontendFS.ReadFile("frontend_dist/favicon.svg")
		c.Data(http.StatusOK, "image/svg+xml", data)
	})

	for _, p := range []string{"/", "/web", "/config", "/jobs", "/stats", "/media", "/preferences", "/db"} {
		r.GET(p, serveSPA)
	}

	// ── Admin REST API ────────────────────────────────────────────────────
	api := r.Group("/api/frontend")
	{
		api.GET("/config", handleConfig)
		api.GET("/status", handleStatus)
		api.GET("/stats", handleStats)
		api.GET("/jobs", handleJobs)
		api.GET("/media-library", handleMediaLibrary)
		api.GET("/media-library/:id", handleMediaLibrarySeries)
		api.POST("/search-episode", handleSearchEpisode)
		api.POST("/apply-subtitle", handleApplySubtitle)
		api.GET("/tmdb/season-count", handleTMDBSeasonCount)

		api.GET("/tmdb/search", handleTMDBSearch)
		registerDBAdminRoutes(api)

		api.GET("/preferences", handlePreferencesList)
		api.POST("/preferences/blacklist", handlePreferencesAddBlacklist)
		api.DELETE("/preferences/blacklist/:hash", handlePreferencesRemoveBlacklist)
		api.POST("/preferences/pin", handlePreferencesSetPin)
		api.DELETE("/preferences/pin", handlePreferencesRemovePin)
	}

	// ── SRN API (used by MediaLibrary subtitle selection modal + SRNManagement) ─
	srnAPI := r.Group("/srn/api")
	{
		srnAPI.GET("/search", handleSRNSearch)
		srnAPI.GET("/events", handleSRNEvents)
		srnAPI.DELETE("/events/:id", handleSRNEventDelete)
	}
}

func handleTMDBSeasonCount(c *gin.Context) {
	idStr := c.Query("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id required"})
		return
	}
	n := tmdb.FetchSeasonCount(id)
	if n < 0 {
		c.JSON(http.StatusOK, gin.H{"count": 1})
		return
	}
	c.JSON(http.StatusOK, gin.H{"count": n})
}

func handleTMDBSearch(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "q required"})
		return
	}
	results, err := tmdb.FetchSeriesSearchResults(q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if results == nil {
		results = []tmdb.TMDBInfo{}
	}
	c.JSON(http.StatusOK, gin.H{"results": results})
}

func serveSPA(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", frontendIndex)
}

func handleConfig(c *gin.Context) {
	alias := config.SRNNodeAlias
	if alias == "" {
		alias = nodePublicKey
	}
	c.JSON(http.StatusOK, gin.H{
		"ProwlarrTargetURL":  config.ProwlarrTargetURL,
		"TargetLanguage":     config.TargetLanguage,
		"TVDBLanguage":       config.TVDBLanguage,
		"CacheDBPath":        config.CacheDBPath,
		"SRNDBPath":          config.SRNDBPath,
		"StateDBPath":        config.StateDBPath,
		"SonarrURL":          config.SonarrURL,
		"SonarrSyncInterval": config.SonarrSyncInterval.String(),
		"BackendSRNURL":      config.BackendSRNURL,
		"SRNRelayURLs":       strings.Join(config.SRNRelayURLs, ", "),
		"SRNNodePublicKey":   nodePublicKey,
		"SRNNodeAlias":       alias,
	})
}

func handleJobs(c *gin.Context) {
	jobs := registeredJobs
	if jobs == nil {
		jobs = []map[string]string{}
	}
	c.JSON(http.StatusOK, gin.H{"jobs": jobs})
}

func handleStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"uptime":     time.Since(startTime).String(),
		"start_time": startTime.Format(time.RFC3339),
		"version":    "1.0.0",
	})
}

func handleStats(c *gin.Context) {
	c.JSON(http.StatusOK, metrics.CurrentJSON())
}

// ── Subtitle Preferences ──────────────────────────────────────────────────────

func handlePreferencesList(c *gin.Context) {
	st := state.GetStore(config.StateDBPath)
	bl, err := st.ListBlacklist()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	pins, err := st.ListPins()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if bl == nil {
		bl = []state.BlacklistEntry{}
	}
	if pins == nil {
		pins = []state.PinEntry{}
	}
	c.JSON(http.StatusOK, gin.H{"blacklist": bl, "pins": pins})
}

func handlePreferencesAddBlacklist(c *gin.Context) {
	var body struct {
		EventHash string `json:"event_hash"`
		CacheKey  string `json:"cache_key"`
		Reason    string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.EventHash == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "event_hash required"})
		return
	}
	if err := state.GetStore(config.StateDBPath).AddBlacklist(body.EventHash, body.CacheKey, body.Reason); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func handlePreferencesRemoveBlacklist(c *gin.Context) {
	hash := c.Param("hash")
	if hash == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "hash required"})
		return
	}
	if err := state.GetStore(config.StateDBPath).RemoveBlacklist(hash); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func handlePreferencesSetPin(c *gin.Context) {
	var body struct {
		CacheKey string `json:"cache_key"`
		EventID  string `json:"event_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.CacheKey == "" || body.EventID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cache_key and event_id required"})
		return
	}
	if err := state.GetStore(config.StateDBPath).SetPin(body.CacheKey, body.EventID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func handlePreferencesRemovePin(c *gin.Context) {
	cacheKey := c.Query("key")
	if cacheKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "key required"})
		return
	}
	if err := state.GetStore(config.StateDBPath).RemovePin(cacheKey); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GET /srn/api/search?tmdb_id=<id>&s=<season>&e=<ep>
// Returns available subtitles from SRN (local → backend → cloud relay).
func handleSRNSearch(c *gin.Context) {
	if globalSonarrSearcher == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Sonarr not configured"})
		return
	}
	tmdbID := strings.TrimSpace(c.Query("tmdb_id"))
	if tmdbID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tmdb_id required"})
		return
	}
	season, _ := strconv.Atoi(c.Query("s"))
	ep, _ := strconv.Atoi(c.Query("e"))
	results := globalSonarrSearcher.QuerySubtitles(tmdbID, season, ep)
	c.JSON(http.StatusOK, results)
}
