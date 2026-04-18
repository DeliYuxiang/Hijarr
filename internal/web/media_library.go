package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"hijarr/internal/config"
	"hijarr/internal/sonarr"
	"hijarr/internal/srn"
	"hijarr/internal/state"
	"hijarr/internal/tmdb"
	"hijarr/internal/util"

	"github.com/gin-gonic/gin"
)

// EpisodeSearcher is implemented by scheduler.SonarrSyncJob.
type EpisodeSearcher interface {
	SearchEpisode(videoPath, tmdbIDStr, localTitle string, season, ep int) string
	SetSubtitleSelection(videoPath, archiveMD5, subMD5 string)
	QuerySubtitles(tmdbIDStr string, season, ep int) []map[string]interface{}
}

var globalSonarrClient *sonarr.Client
var globalSonarrSearcher EpisodeSearcher

// SetSonarrClient injects the Sonarr API client for the media library handler.
func SetSonarrClient(c *sonarr.Client) { globalSonarrClient = c }

// SetSonarrSearcher injects the episode searcher (SonarrSyncJob) for manual search.
func SetSonarrSearcher(s EpisodeSearcher) { globalSonarrSearcher = s }

// EpisodeStatus is the per-episode data returned by the media library detail endpoint.
type EpisodeStatus struct {
	Ep              int    `json:"ep"`
	VideoPath       string `json:"video_path"` // local path (after prefix translation)
	HasSub          bool   `json:"has_sub"`
	SubPath         string `json:"sub_path,omitempty"`
	SubMD5          string `json:"sub_md5,omitempty"`
	SelectedSubMD5  string `json:"selected_sub_md5,omitempty"`
	ArchiveMD5      string `json:"archive_md5,omitempty"`
}

// SeasonStatus aggregates episode data for one season.
type SeasonStatus struct {
	Season   int             `json:"season"`
	Total    int             `json:"total"`
	HasSub   int             `json:"has_sub"`
	Episodes []EpisodeStatus `json:"episodes"`
}

// SeriesDetail is the full detail response for one series.
type SeriesDetail struct {
	SeriesID   int            `json:"series_id"`
	Title      string         `json:"title"`
	LocalTitle string         `json:"local_title"`
	TMDBID     string         `json:"tmdb_id"`
	Seasons    []SeasonStatus `json:"seasons"`
}

// GET /api/frontend/media-library — returns list of all Sonarr series.
func handleMediaLibrary(c *gin.Context) {
	if globalSonarrClient == nil {
		c.JSON(http.StatusOK, gin.H{"configured": false, "series": []interface{}{}})
		return
	}
	series, err := globalSonarrClient.GetAllSeries()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"configured": true, "series": series})
}

// GET /api/frontend/media-library/:id — returns seasons/episodes with subtitle status for one series.
func handleMediaLibrarySeries(c *gin.Context) {
	if globalSonarrClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Sonarr not configured"})
		return
	}
	seriesID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid series id"})
		return
	}

	stateStore := state.GetStore(config.StateDBPath)

	// Locate the series to get title/tmdbID
	allSeries, err := globalSonarrClient.GetAllSeries()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var target *sonarr.Series
	for i := range allSeries {
		if allSeries[i].ID == seriesID {
			target = &allSeries[i]
			break
		}
	}
	if target == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "series not found"})
		return
	}

	// Fetch localized title from TMDB
	localTitle := target.Title
	if target.TmdbID != 0 {
		if info, err := tmdb.FetchSeriesInfoByID(target.TmdbID); err == nil && info != nil && info.Title != "" {
			localTitle = info.Title
		}
	}

	episodes, err := globalSonarrClient.GetEpisodes(seriesID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	episodeFiles, err := globalSonarrClient.GetEpisodeFiles(seriesID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Group by season
	seasonMap := map[int]*SeasonStatus{}
	for _, ep := range episodes {
		if !ep.HasFile {
			continue
		}
		ef, ok := episodeFiles[ep.EpisodeFileID]
		if !ok {
			continue
		}
		localPath := ef.Path
		if config.SonarrPathPrefix != "" && config.LocalPathPrefix != "" {
			localPath = strings.Replace(localPath, config.SonarrPathPrefix, config.LocalPathPrefix, 1)
		}
		subPath := findSubtitleOnDisk(localPath)
		hasSub := subPath != ""

		s, ok := seasonMap[ep.SeasonNumber]
		if !ok {
			s = &SeasonStatus{Season: ep.SeasonNumber}
			seasonMap[ep.SeasonNumber] = s
		}
		s.Total++
		epStatus := EpisodeStatus{
			Ep:        ep.EpisodeNumber,
			VideoPath: localPath,
			HasSub:    hasSub,
			SubPath:   subPath,
		}

		if hasSub {
			s.HasSub++
			epStatus.SubMD5, _ = util.CalculateFileMD5(subPath)
		}

		if sel := stateStore.GetSubtitleSelection(localPath); sel != nil {
			epStatus.SelectedSubMD5 = sel.SubMD5
			epStatus.ArchiveMD5 = sel.ArchiveMD5
		}

		s.Episodes = append(s.Episodes, epStatus)
	}

	// Sort seasons and episodes within each season
	var seasons []SeasonStatus
	for _, s := range seasonMap {
		sort.Slice(s.Episodes, func(i, j int) bool {
			return s.Episodes[i].Ep < s.Episodes[j].Ep
		})
		seasons = append(seasons, *s)
	}
	sort.Slice(seasons, func(i, j int) bool {
		return seasons[i].Season < seasons[j].Season
	})

	c.JSON(http.StatusOK, SeriesDetail{
		SeriesID:   seriesID,
		Title:      target.Title,
		LocalTitle: localTitle,
		TMDBID:     fmt.Sprintf("%d", target.TmdbID),
		Seasons:    seasons,
	})
}

// POST /api/frontend/search-episode — manually trigger subtitle search for one episode.
func handleSearchEpisode(c *gin.Context) {
	if globalSonarrSearcher == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Sonarr not configured"})
		return
	}
	var req struct {
		VideoPath  string `json:"video_path"`
		TMDBID     string `json:"tmdb_id"`
		LocalTitle string `json:"local_title"`
		Season     int    `json:"season"`
		Ep         int    `json:"ep"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.VideoPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "video_path, tmdb_id, local_title, season, ep are required"})
		return
	}
	outcome := globalSonarrSearcher.SearchEpisode(req.VideoPath, req.TMDBID, req.LocalTitle, req.Season, req.Ep)
	c.JSON(http.StatusOK, gin.H{"outcome": outcome})
}

// POST /api/frontend/apply-subtitle — manually apply a specific subtitle from SRN.
func handleApplySubtitle(c *gin.Context) {
	if globalSonarrSearcher == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Sonarr not configured"})
		return
	}
	var req struct {
		VideoPath  string `json:"video_path"`
		EventID    string `json:"event_id"`
		SubMD5     string `json:"sub_md5"`
		ArchiveMD5 string `json:"archive_md5"`
		Filename   string `json:"filename"`
		Language   string `json:"language"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.VideoPath == "" || req.EventID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "video_path and event_id are required"})
		return
	}

	srnStore := srn.GetStore(config.SRNDBPath)

	// Get content from local store or relay
	content, _, err := srnStore.GetContent(req.EventID)
	if err != nil || len(content) == 0 {
		content, err = srn.DownloadFromRelays(req.EventID)
		if err != nil || len(content) == 0 {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to download subtitle content"})
			return
		}
	}

	lang := req.Language
	if lang == "" {
		lang = util.DetectSubtitleLang(req.Filename)
	}
	ext := filepath.Ext(req.Filename)
	if ext == "" {
		ext = ".srt"
	}
	destPath := sonarr.SubtitlePath(req.VideoPath, lang, ext)

	// Write file (overwrite if exists)
	if err := os.WriteFile(destPath, content, 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to write subtitle: %v", err)})
		return
	}

	// Record selection via searcher
	globalSonarrSearcher.SetSubtitleSelection(req.VideoPath, req.ArchiveMD5, req.SubMD5)

	c.JSON(http.StatusOK, gin.H{"ok": true, "path": destPath})
}

// findSubtitleOnDisk returns the path to the Chinese subtitle file next to the video if it exists.
func findSubtitleOnDisk(videoPath string) string {
	base := strings.TrimSuffix(videoPath, filepath.Ext(videoPath))
	for _, tag := range []string{"zh-bilingual", "zh-hans", "zh-hant", "zh", "zh-TW"} {
		for _, ext := range []string{".ass", ".srt", ".ssa"} {
			p := base + "." + tag + ext
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}
