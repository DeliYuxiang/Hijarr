package scheduler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"hijarr/internal/config"
	"hijarr/internal/logger"
	"hijarr/internal/sonarr"
	"hijarr/internal/srn"
	"hijarr/internal/state"
	"hijarr/internal/tmdb"
	"hijarr/internal/util"
)

var sonarrLog = logger.For("sonarr")

const sonarrMaxConcurrency = 3

// syncStats accumulates per-run counters for the Sonarr sync job.
type syncStats struct {
	TotalSeries   int
	EpWithFile    int // episodes that have a video file
	AlreadyHasSub int // subtitle file already exists on disk
	Wrote         int // newly written this run
	Missed        int // no subtitle found / write failed
}

func (st *syncStats) add(other syncStats) {
	st.TotalSeries += other.TotalSeries
	st.EpWithFile += other.EpWithFile
	st.AlreadyHasSub += other.AlreadyHasSub
	st.Wrote += other.Wrote
	st.Missed += other.Missed
}

func (st syncStats) fillRate() string {
	covered := st.AlreadyHasSub + st.Wrote
	if st.EpWithFile == 0 {
		return "0 / 0 (N/A)"
	}
	pct := float64(covered) * 100 / float64(st.EpWithFile)
	return fmt.Sprintf("%d / %d (%.1f%%)", covered, st.EpWithFile, pct)
}

// SonarrSyncJob periodically syncs subtitles from SRN to the Sonarr media library,
// replacing the need for Bazarr as a subtitle manager.
type SonarrSyncJob struct {
	client      *sonarr.Client
	srnStore    *srn.Store
	srnProvider *srn.Provider
}

// NewSonarrSyncJob creates a new SonarrSyncJob with the given Sonarr client.
func NewSonarrSyncJob(client *sonarr.Client) *SonarrSyncJob {
	return &SonarrSyncJob{
		client:      client,
		srnStore:    srn.GetStore(config.SRNDBPath),
		srnProvider: srn.NewProvider(),
	}
}

// Name implements the Job interface.
func (j *SonarrSyncJob) Name() string { return "sonarr_sync" }

// Run implements the Job interface. It iterates all Sonarr series and writes
// subtitle files for any episode that has a video file but is missing subtitles.
func (j *SonarrSyncJob) Run(ctx context.Context) {
	sonarrLog.Info("🔄 [Sonarr同步] 开始同步字幕...\n")

	allSeries, err := j.client.GetAllSeries()
	if err != nil {
		sonarrLog.Warn("⚠️  [Sonarr同步] 获取剧集列表失败: %v\n", err)
		return
	}
	sonarrLog.Info("🔄 [Sonarr同步] 共 %d 部剧集待处理\n", len(allSeries))

	sem := make(chan struct{}, sonarrMaxConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var total syncStats
	total.TotalSeries = len(allSeries)

	for _, s := range allSeries {
		if s.TmdbID == 0 {
			continue
		}
		if ctx.Err() != nil {
			break
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(series sonarr.Series) {
			defer wg.Done()
			defer func() { <-sem }()
			st := j.processSeries(series)
			mu.Lock()
			total.add(st)
			mu.Unlock()
		}(s)
	}
	wg.Wait()

	sonarrLog.Info("\n📊 [Sonarr同步] ══════════════════════════════\n")
	sonarrLog.Info("   媒体库充盈率   %s\n", total.fillRate())
	sonarrLog.Info("   本次新写入     %d 集\n", total.Wrote)
	sonarrLog.Info("   已有字幕跳过   %d 集\n", total.AlreadyHasSub)
	sonarrLog.Info("   仍缺字幕       %d 集\n", total.Missed)
	sonarrLog.Info("📊 [Sonarr同步] ══════════════════════════════\n\n")
}

func (j *SonarrSyncJob) processSeries(series sonarr.Series) syncStats {
	tmdbIDStr := fmt.Sprintf("%d", series.TmdbID)

	// Fetch localized title from TMDB
	localTitle := series.Title
	if info, err := tmdb.FetchSeriesInfoByID(series.TmdbID); err == nil && info != nil && info.Title != "" {
		localTitle = info.Title
	}

	var st syncStats

	episodes, err := j.client.GetEpisodes(series.ID)
	if err != nil {
		sonarrLog.Warn("⚠️  [Sonarr同步] 获取剧集 %q 集列表失败: %v\n", series.Title, err)
		return st
	}

	episodeFiles, err := j.client.GetEpisodeFiles(series.ID)
	if err != nil {
		sonarrLog.Warn("⚠️  [Sonarr同步] 获取剧集 %q 文件列表失败: %v\n", series.Title, err)
		return st
	}

	for _, ep := range episodes {
		if !ep.HasFile {
			continue
		}
		ef, ok := episodeFiles[ep.EpisodeFileID]
		if !ok {
			continue
		}
		st.EpWithFile++

		// Convert Sonarr path prefix to local path prefix
		videoPath := ef.Path
		if config.SonarrPathPrefix != "" && config.LocalPathPrefix != "" {
			videoPath = strings.Replace(videoPath, config.SonarrPathPrefix, config.LocalPathPrefix, 1)
		}

		switch j.processEpisode(videoPath, tmdbIDStr, localTitle, ep.SeasonNumber, ep.EpisodeNumber) {
		case epOutcomeAlreadyHas:
			st.AlreadyHasSub++
		case epOutcomeWrote:
			st.Wrote++
		case epOutcomeMissed:
			st.Missed++
		}
	}

	if st.Wrote > 0 {
		if err := j.client.RescanSeries(series.ID); err != nil {
			sonarrLog.Warn("⚠️  [Sonarr同步] RescanSeries(%d) 失败: %v\n", series.ID, err)
		} else {
			sonarrLog.Debug("🔔 [Sonarr同步] 触发 RescanSeries: %q (id=%d)\n", localTitle, series.ID)
		}
	}
	return st
}

// SearchEpisode runs processEpisode for a single episode and returns the outcome as a string.
// videoPath must already be the LOCAL path (path-prefix translation done by caller).
// Returns "wrote", "already_has", or "missed".
func (j *SonarrSyncJob) SearchEpisode(videoPath, tmdbIDStr, localTitle string, season, ep int) string {
	switch j.processEpisode(videoPath, tmdbIDStr, localTitle, season, ep) {
	case epOutcomeAlreadyHas:
		return "already_has"
	case epOutcomeWrote:
		return "wrote"
	default:
		return "missed"
	}
}

// QuerySubtitles searches SRN (local → backend → cloud relay) for available subtitles
// for the given episode. Returns an empty slice when nothing is found.
func (j *SonarrSyncJob) QuerySubtitles(tmdbIDStr string, season, ep int) []map[string]interface{} {
	// Use tmdbIDStr as placeholder title: ParseCacheKey requires non-empty title,
	// but store.Query ignores title when tmdbID is present, so this is safe.
	cacheKey := fmt.Sprintf("%s|T%s|S%d|E%d", tmdbIDStr, tmdbIDStr, season, ep)
	results, err := j.srnProvider.SearchByCacheKey(cacheKey, nil)
	if err != nil {
		sonarrLog.Warn("⚠️  [SRN] QuerySubtitles 查询失败: %v\n", err)
	}
	if results == nil {
		return []map[string]interface{}{}
	}
	return results
}

// SetSubtitleSelection records the user's choice of subtitle for a video path.
func (j *SonarrSyncJob) SetSubtitleSelection(videoPath, archiveMD5, subMD5 string) {
	state.GetStore(config.StateDBPath).SetSubtitleSelection(videoPath, archiveMD5, subMD5)
}

type epOutcome int

const (
	epOutcomeMissed     epOutcome = iota // no subtitle found or write failed
	epOutcomeAlreadyHas                  // subtitle already exists on disk
	epOutcomeWrote                       // newly written this run
)

// hasExistingSubtitle checks whether a subtitle file already exists next to the video
// for any of the configured SRN_PREFERRED_LANGUAGES.
func hasExistingSubtitle(videoPath string) bool {
	base := strings.TrimSuffix(videoPath, filepath.Ext(videoPath))
	tagSet := map[string]bool{}
	for _, lang := range config.SRNPreferredLanguages {
		for _, tag := range lang.SonarrFileSuffixes() {
			tagSet[tag] = true
		}
	}
	if len(tagSet) == 0 {
		tagSet["zh"] = true
		tagSet["zh-TW"] = true
	}
	for tag := range tagSet {
		for _, ext := range []string{".ass", ".srt", ".ssa"} {
			if _, err := os.Stat(base + "." + tag + ext); err == nil {
				return true
			}
		}
	}
	return false
}

// processEpisode finds or fetches a subtitle for the episode and writes it next to the video file.
func (j *SonarrSyncJob) processEpisode(
	videoPath, tmdbIDStr, localTitle string,
	season, ep int,
) epOutcome {
	// Fast path: subtitle already on disk
	if hasExistingSubtitle(videoPath) {
		return epOutcomeAlreadyHas
	}

	// Query SRN (Local cache + Backend + Cloud)
	cacheKey := fmt.Sprintf("%s|T%s|S%d|E%d", localTitle, tmdbIDStr, season, ep)
	results, err := j.srnProvider.SearchByCacheKey(cacheKey, nil)
	if err != nil {
		sonarrLog.Warn("⚠️  [Sonarr同步] SRN 查询失败: %v\n", err)
	}

	if len(results) == 0 {
		return epOutcomeMissed
	}

	for _, r := range results {
		idRaw := fmt.Sprint(r["id"])
		if !strings.HasPrefix(idRaw, "srn_") {
			continue // Should not happen with SRN provider
		}
		eventID := strings.TrimPrefix(idRaw, "srn_")
		filename := fmt.Sprint(r["native_name"])
		lang := fmt.Sprint(r["language"])

		if lang == "" || lang == "<nil>" {
			lang = util.DetectSubtitleLang(filename)
		}

		ext := strings.ToLower(filepath.Ext(filename))
		if ext == "" {
			ext = ".srt"
		}

		destPath := sonarr.SubtitlePath(videoPath, lang, ext)

		// Try local store first
		content, _, err := j.srnStore.GetContent(eventID)
		if err != nil || len(content) == 0 {
			// Fallback to remote relays
			content, err = srn.DownloadFromRelays(eventID)
			if err != nil || len(content) == 0 {
				continue
			}
		}

		// O_CREATE|O_EXCL: atomic skip if already exists (idempotent)
		f, err := os.OpenFile(destPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if os.IsExist(err) {
			return epOutcomeAlreadyHas
		}
		if err != nil {
			sonarrLog.Warn("⚠️  [Sonarr同步] 写入字幕失败 %s: %v\n", destPath, err)
			continue
		}
		_, writeErr := f.Write(content)
		f.Close()
		if writeErr != nil {
			sonarrLog.Warn("⚠️  [Sonarr同步] 写入字幕失败 %s: %v\n", destPath, writeErr)
			os.Remove(destPath)
			continue
		}

		// Record selection
		j.SetSubtitleSelection(videoPath, fmt.Sprint(r["archive_md5"]), eventID)

		sonarrLog.Debug("💾 [Sonarr同步] 写入字幕: %s\n", destPath)
		return epOutcomeWrote
	}
	return epOutcomeMissed
}
