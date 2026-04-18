package srn

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"hijarr/internal/config"
	"hijarr/internal/logger"
	"hijarr/internal/metrics"
)

var log = logger.For("srn-store")

// Provider implements subtitles.SubtitleProvider + subtitles.SemanticProvider.
// Priority: local SQLite store first, then remote relay network.
type Provider struct {
	store *Store
}

// NewProvider returns the SRN subtitle provider backed by the global local store.
func NewProvider() *Provider {
	return &Provider{store: GetStore(config.SRNDBPath)}
}

func (p *Provider) Name() string { return "srn" }

func (p *Provider) Search(_ url.Values, _ string) ([]map[string]interface{}, error) {
	return nil, nil // SRN uses SearchByCacheKey
}

// SearchByCacheKey parses a key in the form "title|Sn|En" and queries with priority:
// 1. Local srn_events (instant, no network round-trip)
// 2. Remote relay network (if local misses and relays are configured)
func (p *Provider) SearchByCacheKey(key string, _ url.Values) ([]map[string]interface{}, error) {
	title, tmdbID, season, ep := ParseCacheKey(key)
	if title == "" {
		return nil, nil
	}

	metrics.SRNQueryTotal.Add(1)

	// Priority 1: Local store — instant, no network dependency
	if p.store != nil {
		results, err := p.store.Query(tmdbID, title, season, ep)
		if err == nil && len(results) > 0 {
			metrics.SRNQueryHit.Add(1)
			subs := make([]map[string]interface{}, 0, len(results))
			for _, r := range results {
				subs = append(subs, map[string]interface{}{
					"id":          "srn_" + r.ID,
					"native_name": r.Filename,
					"videoname":   fmt.Sprintf("%s S%02dE%02d", r.Title, r.Season, r.Ep),
					"language":    r.Lang,
					"_srn":        true,
				})
			}
			log.Debug("🏠 [SRN] 本地命中 %d 条, key=%q\n", len(subs), key)
			return subs, nil
		}
	}

	// Priority 2: Backend-SRN (local srnfeeder instance — lower latency than cloud)
	if config.BackendSRNURL != "" {
		var backendEvents []Event
		for _, lang := range config.SRNPreferredLanguages {
			evs, err := queryOne(config.BackendSRNURL, tmdbID, string(lang), season, ep)
			if err != nil {
				log.Warn("⚠️  [SRN] Backend-SRN query 失败 (%s): %v\n", config.BackendSRNURL, err)
				break
			}
			mergeEvents(&backendEvents, evs)
		}
		if len(backendEvents) > 0 {
			metrics.SRNQueryHit.Add(1)
			subs := make([]map[string]interface{}, 0, len(backendEvents))
			for _, ev := range backendEvents {
				subs = append(subs, map[string]interface{}{
					"id":          "srn_" + ev.ID,
					"native_name": ev.Filename,
					"videoname":   fmt.Sprintf("%s S%02dE%02d", title, season, ep),
					"language":    ev.GetTag("language"),
					"_srn":        true,
					"_backend":    true,
				})
			}
			log.Debug("🏭 [SRN] Backend 命中 %d 条, key=%q\n", len(subs), key)
			return subs, nil
		}
	}

	// Priority 3: Cloud relay network
	if len(config.SRNRelayURLs) == 0 {
		return nil, nil
	}
	remoteEvents := QueryNetworkForLangs(tmdbID, config.SRNPreferredLanguages, season, ep)
	if len(remoteEvents) == 0 {
		return nil, nil
	}

	metrics.SRNQueryHit.Add(1)
	subs := make([]map[string]interface{}, 0, len(remoteEvents))
	for _, ev := range remoteEvents {
		subs = append(subs, map[string]interface{}{
			"id":          "srn_" + ev.ID,
			"native_name": ev.Filename,
			"videoname":   fmt.Sprintf("%s S%02dE%02d", title, season, ep),
			"language":    ev.GetTag("language"),
			"_srn":        true,
			"_remote":     true,
		})
	}
	log.Debug("🌐 [SRN] 云端命中 %d 条, key=%q\n", len(subs), key)
	return subs, nil
}

var (
	srnSeasonKeyRe  = regexp.MustCompile(`\|S(\d+)`)
	srnEpisodeKeyRe = regexp.MustCompile(`\|E(\d+)`)
	srnTmdbKeyRe    = regexp.MustCompile(`\|T(\d+)`)
)

// SetSRNKeyPatterns (HIJARR CORE ASSET: Identification/Cleaning logic)
func SetSRNKeyPatterns(season, episode []string) {
	if len(season) > 0 {
		if re, err := regexp.Compile(season[0]); err == nil {
			srnSeasonKeyRe = re
		}
	}
	if len(episode) > 0 {
		if re, err := regexp.Compile(episode[0]); err == nil {
			srnEpisodeKeyRe = re
		}
	}
}

// ParseCacheKey extracts title, tmdbID, season and ep from a cache key.
// Supported formats:
//   - "title|T<tmdbID>|S<n>|E<n>" (new, preferred)
//   - "title|S<n>|E<n>" (legacy, tmdbID returns "")
func ParseCacheKey(key string) (title, tmdbID string, season, ep int) {
	if m := srnTmdbKeyRe.FindStringSubmatch(key); len(m) == 2 {
		tmdbID = m[1]
		key = srnTmdbKeyRe.ReplaceAllString(key, "")
	}
	if m := srnSeasonKeyRe.FindStringSubmatch(key); len(m) == 2 {
		season, _ = strconv.Atoi(m[1])
		key = srnSeasonKeyRe.ReplaceAllString(key, "")
	}
	if m := srnEpisodeKeyRe.FindStringSubmatch(key); len(m) == 2 {
		ep, _ = strconv.Atoi(m[1])
		key = srnEpisodeKeyRe.ReplaceAllString(key, "")
	}
	title = strings.TrimRight(key, "|")
	return title, tmdbID, season, ep
}
