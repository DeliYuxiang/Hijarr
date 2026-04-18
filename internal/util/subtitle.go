package util

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// CalculateMD5 returns the hex MD5 hash of the given data.
func CalculateMD5(data []byte) string {
	h := md5.Sum(data)
	return hex.EncodeToString(h[:])
}

// CalculateFileMD5 returns the hex MD5 hash of the file at the given path.
func CalculateFileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}


// FilenameFromURL 从原始 URL 提取文件名：先剥离 query string，再 filepath.Base，最后 URL decode。
// 用于将 "https://host/path/foo%20bar.zip?token=xxx" 统一转为 "foo bar.zip"。
func FilenameFromURL(rawURL string) string {
	u := rawURL
	if idx := strings.Index(u, "?"); idx != -1 {
		u = u[:idx]
	}
	name := filepath.Base(u)
	if decoded, err := url.PathUnescape(name); err == nil {
		name = decoded
	}
	return name
}

// SubtitleExts 是所有受支持的字幕文件扩展名。
var SubtitleExts = map[string]bool{
	".srt": true, ".ass": true, ".ssa": true, ".sub": true,
}

// IsSubtitleFile 报告给定文件名是否为字幕文件。
func IsSubtitleFile(name string) bool {
	return SubtitleExts[strings.ToLower(filepath.Ext(name))]
}

// JunkTitleKeywords 是明确无字幕价值的包名/文件名关键词（不区分大小写）。
var JunkTitleKeywords = []string{
	"font", "fonts", "字体",
	"wikipedia", "wiki",
	"ncop", "nced", "creditless",
	"wallpaper", "壁纸",
	"artbook", "art book", "画集",
	"ost", "soundtrack", "原声",
	"patch", "v2patch", "v3patch", "mp3", "extras", "danmu", "rename", "manga",
}

// IsJunkTitle 判断 title/filename 是否命中垃圾包黑名单（不区分大小写）。
func IsJunkTitle(title string) bool {
	lower := strings.ToLower(title)
	for _, kw := range JunkTitleKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// DetectSubtitleLang 从文件名推断字幕语言标签。
// 返回值：
//
//	"zh-hans"      简体中文  (.sc. / [CHS] / 简体 / 简中)
//	"zh-hant"      繁体中文  (.tc. / [CHT] / 繁体 / 繁中)
//	"zh-bilingual" 双语      (双语 / 简繁 / jc / 日中 / sc&tc)
//	"zh"           未知/通用中文（兜底）
func DetectSubtitleLang(name string) string {
	lower := strings.ToLower(name)

	// Bilingual markers take priority (often appear before sc/tc in name)
	if strings.Contains(lower, "双语") ||
		strings.Contains(lower, "简繁") ||
		strings.Contains(lower, "繁简") ||
		strings.Contains(lower, ".jc.") ||
		strings.Contains(lower, "_jc.") ||
		strings.Contains(lower, ".scjp.") ||
		strings.Contains(lower, "_scjp.") ||
		strings.Contains(lower, ".tcjp.") ||
		strings.Contains(lower, "_tcjp.") ||
		strings.Contains(lower, "日中") ||
		strings.Contains(lower, "中日") ||
		// e.g. "sc&tc" or "sc+tc"
		(strings.Contains(lower, "sc") && strings.Contains(lower, "tc")) {
		return "zh-bilingual"
	}

	// Simplified Chinese
	if strings.Contains(lower, "zh-hans") ||
		strings.Contains(lower, ".sc.") ||
		strings.Contains(lower, "_sc.") ||
		strings.Contains(lower, "[chs]") ||
		strings.Contains(lower, ".chs.") ||
		strings.Contains(lower, "_chs.") ||
		strings.Contains(lower, "简体") ||
		strings.Contains(lower, "简中") ||
		strings.Contains(lower, ".gb.") {
		return "zh-hans"
	}

	// Traditional Chinese
	if strings.Contains(lower, "zh-hant") ||
		strings.Contains(lower, ".tc.") ||
		strings.Contains(lower, "_tc.") ||
		strings.Contains(lower, "[cht]") ||
		strings.Contains(lower, ".cht.") ||
		strings.Contains(lower, "_cht.") ||
		strings.Contains(lower, "繁体") ||
		strings.Contains(lower, "繁中") ||
		strings.Contains(lower, ".big5.") {
		return "zh-hant"
	}

	return "zh"
}

// FileCategory classifies a subtitle file for recognition routing.
type FileCategory int

const (
	CategoryNormal  FileCategory = iota // regular TV episode
	CategorySeason0                     // OVA/OAD/SP → S0
	CategoryExtras                      // NCOP/PV/CM etc. → skip recognition
	CategorySkip                        // non-subtitle extension or junk title
)

// extrasMarkers are lowercase substrings that indicate credit-less or
// promotional content carrying no episode meaning.
// Open-bracket prefixes (no closing bracket) are intentional: they match both
// plain tags ([CM], [Trailer]) and numbered variants ([CM04], [Trailer01]).
var extrasMarkers = []string{
	"ncop", "ncoa", "nced", "ncoed", "npcd", "npoa",
	"creditless",
	"[cm", "(cm",
	"[pv", "(pv",
	"[iv", "(iv",
	"[menu", "(menu", "bd menu",
	"[trailer", "(trailer",
	"[preview", "(preview",
}

// season0Markers are lowercase substrings that indicate OVA/OAD/Special
// content to be filed under Season 0.
var season0Markers = []string{
	"[ova", "(ova", // matches [OVA], [OVA01], [OVA 01] …
	"[oad", "(oad",
	"[sp]", "(sp)", "[sp0", "[sp1", "[sp2", "[sp3", // [SP], [SP01] …
	"special", // Special, Specials, [Special]
}

// ClassifySubtitleFile determines how a subtitle file should be routed for
// recognition.  The decision is based solely on the filename (not the path).
//
// Order matters: extrasMarkers is checked before IsJunkTitle because several
// junk keywords (ncop, nced, creditless) overlap with extras — a subtitle
// file carrying those tags is valid content, just non-episode.
func ClassifySubtitleFile(name string) FileCategory {
	if !IsSubtitleFile(name) {
		return CategorySkip
	}
	lower := strings.ToLower(name)
	// Extras check first: these are legitimate subtitle files for
	// non-episode production content (credit-less OP/ED, PV, CM …)
	for _, m := range extrasMarkers {
		if strings.Contains(lower, m) {
			return CategoryExtras
		}
	}
	// Generic junk (font packs, music archives, renamer tools with .ass ext)
	if IsJunkTitle(name) {
		return CategorySkip
	}
	for _, m := range season0Markers {
		if strings.Contains(lower, m) {
			return CategorySeason0
		}
	}
	return CategoryNormal
}

// StripASSAttachments removes [Fonts] and [Graphics] sections from ASS subtitle
// data. These sections contain UUencoded font/image data embedded by BD release
// groups (e.g. Moozzi2), inflating files from ~200 KB to 25 MB+. The subtitle
// text ([Events] section) is preserved intact. Non-ASS data is returned unchanged.
func StripASSAttachments(data []byte) []byte {
	probe := data
	if len(probe) > 512 {
		probe = probe[:512]
	}
	if !bytes.Contains(probe, []byte("[Script Info]")) {
		return data
	}

	var out bytes.Buffer
	out.Grow(len(data))

	scanner := bufio.NewScanner(bytes.NewReader(data))
	// UUencoded font attachment lines can be very long; raise scanner limit.
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	skip := false
	for scanner.Scan() {
		line := scanner.Bytes()
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) >= 2 && trimmed[0] == '[' && trimmed[len(trimmed)-1] == ']' {
			sec := string(trimmed)
			skip = sec == "[Fonts]" || sec == "[Graphics]"
		}
		if !skip {
			out.Write(line)
			out.WriteByte('\n')
		}
	}
	return out.Bytes()
}

// DeduplicateStrings 对字符串切片去重（保留顺序），跳过空字符串。
func DeduplicateStrings(ss []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range ss {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
// ExtractGroup attempts to identify the release group from a filename (usually first brackets).
func ExtractGroup(name string) string {
	// Look for [Group] at the start
	if strings.HasPrefix(name, "[") {
		idx := strings.Index(name, "]")
		if idx > 1 {
			return name[1:idx]
		}
	}
	return ""
}

// ExtractResolution attempts to identify quality markers like 1080p, 720p, etc.
func ExtractResolution(name string) string {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "2160p") || strings.Contains(lower, "4k") {
		return "2160p"
	}
	if strings.Contains(lower, "1080p") {
		return "1080p"
	}
	if strings.Contains(lower, "720p") {
		return "720p"
	}
	if strings.Contains(lower, "540p") {
		return "540p"
	}
	if strings.Contains(lower, "480p") {
		return "480p"
	}
	return ""
}
