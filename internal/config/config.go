package config

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SubtitleLanguage 是 SRN relay 的语言标签枚举。
// 值与 hijarr internal/util.DetectSubtitleLang 输出对齐，
// 也是 relay event_metadata.language 字段的实际存储值。
//
// relay 查询规则（events.ts）：
//   - 不含 "-" 的值（如 "zh"）使用 LIKE 前缀匹配，覆盖所有子变体
//   - 含 "-" 的值（如 "zh-hans"）使用精确匹配
type SubtitleLanguage string

const (
	// LangZH 任意中文（前缀匹配，覆盖 zh-hans/zh-hant/zh-bilingual/zh）
	LangZH SubtitleLanguage = "zh"
	// LangZHHans 简体中文（对应 DetectSubtitleLang 返回值 "zh-hans"）
	LangZHHans SubtitleLanguage = "zh-hans"
	// LangZHHant 繁体中文（对应 DetectSubtitleLang 返回值 "zh-hant"）
	LangZHHant SubtitleLanguage = "zh-hant"
	// LangZHBilingual 双语字幕，通常为中日（对应 DetectSubtitleLang 返回值 "zh-bilingual"）
	LangZHBilingual SubtitleLanguage = "zh-bilingual"
	// LangEN 英文
	LangEN SubtitleLanguage = "en"
)

var validSubtitleLanguages = map[SubtitleLanguage]bool{
	LangZH: true, LangZHHans: true, LangZHHant: true, LangZHBilingual: true, LangEN: true,
}

// SonarrFileSuffixes 返回此语言标签在磁盘上对应的 Sonarr 字幕文件后缀列表。
// Sonarr 约定：
//   - 简体/双语/通用中文 → ".zh"
//   - 繁体中文           → ".zh-TW"
//   - LangZH（任意中文） → [".zh", ".zh-TW"]（检测两种）
//   - 英文               → ".en"
func (l SubtitleLanguage) SonarrFileSuffixes() []string {
	switch l {
	case LangZHHant:
		return []string{"zh-TW"}
	case LangEN:
		return []string{"en"}
	case LangZH:
		return []string{"zh", "zh-TW"} // 前缀匹配需检测两种磁盘形式
	default: // zh-hans, zh-bilingual → 写为 "zh"
		return []string{"zh"}
	}
}

// parseAndValidateLanguages 解析逗号分隔的语言列表并校验合法性。
// 非法值输出到 stderr 并跳过；若解析结果为空则返回默认值 [LangZH]。
func parseAndValidateLanguages(s string) []SubtitleLanguage {
	var out []SubtitleLanguage
	var invalid []string
	seen := map[SubtitleLanguage]bool{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lang := SubtitleLanguage(part)
		if !validSubtitleLanguages[lang] {
			invalid = append(invalid, part)
			continue
		}
		if !seen[lang] {
			seen[lang] = true
			out = append(out, lang)
		}
	}
	if len(invalid) > 0 {
		var valid []string
		for l := range validSubtitleLanguages {
			valid = append(valid, string(l))
		}
		sort.Strings(valid)
		fmt.Fprintf(os.Stderr, "❌ [Config] SRN_PREFERRED_LANGUAGES 包含无效语言: %s\n   有效值: %s\n",
			strings.Join(invalid, ", "), strings.Join(valid, ", "))
	}
	if len(out) == 0 {
		return []SubtitleLanguage{LangZH}
	}
	return out
}

// SeasonMap defines manual season overrides for weirdly-named multi-season shows.
type SeasonMap struct {
	IncludeTitle bool
	Seasons      map[int]string // season number → display alias used in search queries
}

// ManualSeasonOverrides maps Chinese series titles to their season alias rules.
// Used by the Torznab and Assrt proxy layers to build correct search queries.
var ManualSeasonOverrides = map[string]SeasonMap{
	"物语系列": {
		IncludeTitle: false,
		Seasons: map[int]string{
			1: "化物语",
			2: "伪物语",
			3: "第二季",
			4: "终物语",
			5: "终物语SP",
			6: "第外季&第怪季",
		},
	},
	"鬼灭之刃": {
		IncludeTitle: true,
		Seasons: map[int]string{
			2: "游郭篇 无限列车篇",
			3: "刀匠村篇",
			4: "柱训练篇",
		},
	},
	"诸神之黄昏": {
		IncludeTitle: true,
		Seasons: map[int]string{
			2: "貳之章",
		},
	},
	"炎炎消防队": {
		IncludeTitle: true,
		Seasons: map[int]string{
			2: "貳之章",
		},
	},
}

var (
	Port                   = getEnv("PORT", "8001")
	ProwlarrTargetURL      = getEnv("PROWLARR_TARGET_URL", "http://prowlarr:9696/2/api")

	ProwlarrAPIKey         = getEnv("PROWLARR_API_KEY", "")
	TMDBAPIKey             = getEnv("TMDB_API_KEY", "")
	TargetLanguage         = getEnv("TARGET_LANGUAGE", "zh-CN")
	TVDBLanguage           = getEnv("TVDB_LANGUAGE", "zho")
	SubtitleSearchTimeout  = getEnvDuration("SUBTITLE_SEARCH_TIMEOUT", 3*time.Second)

	CacheDBPath            = getEnv("CACHE_DB_PATH", "/data/hijarr.db")
	SRNDBPath              = getEnv("SRN_DB_PATH", CacheDBPath)
	StateDBPath            = getEnv("STATE_DB_PATH", CacheDBPath)
	// LocalDownloadPaths is reserved for srnfeeder compatibility.
	// DiskScanJob has been removed from hijarr; this variable is kept so that
	// srnfeeder can inherit the same env var without a rename.
	LocalDownloadPaths = getEnv("LOCAL_DOWNLOAD_PATHS", "")

	SonarrURL              = getEnv("SONARR_URL", "")
	SonarrAPIKey           = getEnv("SONARR_API_KEY", "")
	SonarrSyncInterval     = getEnvDuration("SONARR_SYNC_INTERVAL", 5*time.Minute)
	SonarrPathPrefix       = getEnv("SONARR_PATH_PREFIX", "")
	LocalPathPrefix        = getEnv("LOCAL_PATH_PREFIX", "")
	// BackendSRNURL is the URL of the local srnfeeder Backend-SRN instance.
	// When set, the SRN provider queries it as Priority 2 (before cloud relays).
	// Example: "http://srnfeeder:8002"
	BackendSRNURL = getEnv("BACKEND_SRN_URL", "")

	SRNRelayURLs = filterEmpty(strings.Split(getEnv("SRN_RELAY_URLS", ""), ","))
	// SRN_PREFERRED_LANGUAGES 指定向 relay 查询时的语言优先级（逗号分隔，有序）。
	// 默认 "zh"（前缀匹配所有中文变体）。可选值: zh, zh-hans, zh-hant, zh-bilingual, en
	SRNPreferredLanguages = parseAndValidateLanguages(getEnv("SRN_PREFERRED_LANGUAGES", "zh"))

	// SRN_PRIV_KEY 是节点 Ed25519 私钥的 hex 编码（128 个十六进制字符）。
	// 优先级：SRN_PRIV_KEY 环境变量 > global_state 数据库 > 自动生成（并警告）。
	// 生产环境必须通过此变量固化密钥，否则每次新容器/新 DB 都会轮换身份。
	SRNPrivKey = getEnv("SRN_PRIV_KEY", "")

	// SRN_NODE_ALIAS 是节点在 SRN 网络中的可读代号（可选，默认回退到公钥 hex）。
	SRNNodeAlias = getEnv("SRN_NODE_ALIAS", "")
)




func getEnv(key, fallback string) string {

	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v, exists := os.LookupEnv(key); exists {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v, exists := os.LookupEnv(key); exists {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			return i
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v, exists := os.LookupEnv(key); exists {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func filterEmpty(ss []string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
