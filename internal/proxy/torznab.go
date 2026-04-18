package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"hijarr/internal/config"
	"hijarr/internal/tmdb"

	"github.com/beevik/etree"
	"github.com/gin-gonic/gin"
)

var prowlarrClient = &http.Client{Timeout: 15 * time.Second}

// BuildTorznabQuery constructs a Prowlarr/Torznab search query string from a
// Chinese title, season, and episode number. It applies ManualSeasonOverrides
// and standard S/E suffix rules — the same logic used by TorznabProxy when
// Sonarr issues a tvsearch request.
func BuildTorznabQuery(chineseTitle string, season, ep int) string {
	currentQ := chineseTitle

	if sMap, exists := config.ManualSeasonOverrides[chineseTitle]; exists {
		if season > 0 {
			if seasonStr, ok := sMap.Seasons[season]; ok {
				if sMap.IncludeTitle {
					currentQ = fmt.Sprintf("%s %s", chineseTitle, seasonStr)
				} else {
					currentQ = seasonStr
				}
			}
		}
	} else {
		suffix := ""
		qLower := strings.ToLower(currentQ)

		if season > 0 {
			sNum := fmt.Sprintf("%d", season)
			sNumPadded := fmt.Sprintf("%02d", season)
			patterns := []string{
				"s" + sNum, "s" + sNumPadded,
				"第" + sNum + "季", "season " + sNum,
			}
			matched := false
			for _, p := range patterns {
				if strings.Contains(qLower, p) {
					matched = true
					break
				}
			}
			if !matched {
				suffix += fmt.Sprintf(" S%d", season)
			}
		}

		if ep > 0 {
			eNum := fmt.Sprintf("%d", ep)
			eNumPadded := fmt.Sprintf("%02d", ep)
			patterns := []string{
				"e" + eNum, "e" + eNumPadded, "第" + eNum + "集",
			}
			matched := false
			for _, p := range patterns {
				if strings.Contains(qLower, p) {
					matched = true
					break
				}
			}
			if !matched {
				suffix += fmt.Sprintf(" E%s", eNumPadded)
			}
		}

		currentQ = strings.TrimSpace(currentQ + suffix)
	}

	return currentQ
}

// ProwlarrItem represents a single search result from Prowlarr.
type ProwlarrItem struct {
	Title     string
	MagnetURL string
	GUID      string
}

// FissionSearchOptions configures a Prowlarr fission search.
type FissionSearchOptions struct {
	ChineseTitle string // If provided, skips TMDB translation.
	TVDBID       string
	IMDBID       string
	OriginalQ    string // Fallback query if Chinese search fails.
	Season       int
	Episode      int
}

// ExecuteProwlarrFissionSearch performs a tiered search on Prowlarr (Episode -> Season -> Series).
// It returns structural results and the raw XML document for the final successful search.
func ExecuteProwlarrFissionSearch(opts FissionSearchOptions) ([]ProwlarrItem, *etree.Document, error) {
	chineseTitle := opts.ChineseTitle

	// 1. Translate if needed
	if chineseTitle == "" {
		if opts.TVDBID != "" {
			if info, _ := tmdb.FetchSeriesInfo(opts.TVDBID, "tvdb_id"); info != nil {
				chineseTitle = info.Title
			}
		} else if opts.IMDBID != "" {
			if info, _ := tmdb.FetchSeriesInfo(opts.IMDBID, "imdb_id"); info != nil {
				chineseTitle = info.Title
			}
		} else if opts.OriginalQ != "" {
			if title, _ := tmdb.FetchChineseTitleByQuery(opts.OriginalQ); title != "" {
				chineseTitle = title
			}
		}
	}

	// 2. Build Fission levels: Episode -> Season -> Series
	type level struct {
		query string
		label string
	}
	var levels []level

	if chineseTitle != "" {
		if opts.Episode > 0 {
			levels = append(levels, level{BuildTorznabQuery(chineseTitle, opts.Season, opts.Episode), "集"})
		}
		if opts.Season > 0 {
			levels = append(levels, level{BuildTorznabQuery(chineseTitle, opts.Season, 0), "季"})
		}
		levels = append(levels, level{BuildTorznabQuery(chineseTitle, 0, 0), "系列"})
	}

	// Always add the original query as the final fallback
	if opts.OriginalQ != "" && opts.OriginalQ != chineseTitle {
		levels = append(levels, level{opts.OriginalQ, "原始"})
	}

	// 3. Execute tiered search
	var lastDoc *etree.Document
	var lastItems []ProwlarrItem

	for _, lv := range levels {
		params := url.Values{}
		params.Set("t", "search")
		params.Set("q", lv.query)
		params.Set("apikey", config.ProwlarrAPIKey)

		doc, _, err := fetchProwlarrInternal(params)
		if err != nil {
			continue
		}

		items := parseProwlarrItems(doc)
		lastDoc = doc
		lastItems = items

		// If we found enough results (>= 10), stop fission
		if len(items) >= 10 {
			break
		}
	}

	return lastItems, lastDoc, nil
}

func fetchProwlarrInternal(qParams url.Values) (*etree.Document, []byte, error) {
	reqURL := fmt.Sprintf("%s?%s", config.ProwlarrTargetURL, qParams.Encode())
	resp, err := prowlarrClient.Get(reqURL)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(bodyBytes); err != nil {
		return nil, bodyBytes, nil
	}
	return doc, bodyBytes, nil
}

func parseProwlarrItems(doc *etree.Document) []ProwlarrItem {
	if doc == nil {
		return nil
	}
	var items []ProwlarrItem
	for _, el := range doc.FindElements("//item") {
		title := ""
		if t := el.FindElement("title"); t != nil {
			title = t.Text()
		}
		magnet := ""
		guidStr := ""
		if guid := el.FindElement("guid"); guid != nil {
			guidStr = guid.Text()
			if strings.HasPrefix(guidStr, "magnet:") {
				magnet = guidStr
			}
		}
		// Fallback for magnet in enclosure or link if guid is not magnet
		if magnet == "" {
			if enc := el.FindElement("enclosure"); enc != nil {
				if u := enc.SelectAttrValue("url", ""); strings.HasPrefix(u, "magnet:") {
					magnet = u
				}
			}
		}
		items = append(items, ProwlarrItem{
			Title:     title,
			MagnetURL: magnet,
			GUID:      guidStr,
		})
	}
	return items
}

func TorznabProxy(c *gin.Context) {
	params := c.Request.URL.Query()
	searchType := params.Get("t")

	if searchType != "tvsearch" && searchType != "movie" {
		params.Set("apikey", config.ProwlarrAPIKey)
		_, raw, _ := fetchProwlarrInternal(params)
		c.Data(200, "application/xml", raw)
		return
	}

	season, _ := strconv.Atoi(params.Get("season"))
	ep, _ := strconv.Atoi(params.Get("ep"))

	opts := FissionSearchOptions{
		TVDBID:    params.Get("tvdbid"),
		IMDBID:    params.Get("imdbid"),
		OriginalQ: params.Get("q"),
		Season:    season,
		Episode:   ep,
	}

	log.Debug("🔍 [Torznab] %s 请求 tvdbid=%q imdbid=%q q=%q season=%d ep=%d\n",
		searchType, opts.TVDBID, opts.IMDBID, opts.OriginalQ, opts.Season, opts.Episode)

	// No search parameters — forward directly to Prowlarr (capability check / browse-all)
	if opts.TVDBID == "" && opts.IMDBID == "" && opts.OriginalQ == "" {
		log.Debug("🔧 [Torznab] 无有效搜索参数，直接透传至 Prowlarr (rid=%q)\n", params.Get("rid"))
		params.Set("apikey", config.ProwlarrAPIKey)
		_, raw, _ := fetchProwlarrInternal(params)
		c.Data(200, "application/xml", raw)
		return
	}

	_, doc, err := ExecuteProwlarrFissionSearch(opts)
	if err != nil {
		c.String(500, err.Error())
		return
	}

	if doc != nil {
		// Post-process XML for Sonarr (fix magnet links, etc.)
		items := doc.FindElements("//item")
		for _, item := range items {
			magnetLink := ""
			if guid := item.FindElement("guid"); guid != nil {
				if strings.HasPrefix(guid.Text(), "magnet:") {
					magnetLink = guid.Text()
				}
			}

			if magnetLink != "" {
				if link := item.FindElement("link"); link != nil {
					link.SetText(magnetLink)
				}
				if enclosure := item.FindElement("enclosure"); enclosure != nil {
					enclosure.CreateAttr("url", magnetLink)
					enclosure.CreateAttr("type", "application/x-bittorrent")
				}
			}
		}

		var buf bytes.Buffer
		doc.WriteTo(&buf)
		c.Data(200, "application/rss+xml", buf.Bytes())
		return
	}

	c.String(404, "No results found")
}
