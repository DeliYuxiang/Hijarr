package sonarr

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

// Series represents a Sonarr series record.
type Series struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Path   string `json:"path"`
	TmdbID int    `json:"tmdbId"`
}

// Episode represents a single Sonarr episode record.
type Episode struct {
	ID            int  `json:"id"`
	SeasonNumber  int  `json:"seasonNumber"`
	EpisodeNumber int  `json:"episodeNumber"`
	HasFile       bool `json:"hasFile"`
	EpisodeFileID int  `json:"episodeFileId"`
}

// EpisodeFile holds the file path for a Sonarr episode file.
type EpisodeFile struct {
	ID           int    `json:"id"`
	Path         string `json:"path"`
	RelativePath string `json:"relativePath"`
}

// Client is a minimal Sonarr v3 API client.
type Client struct {
	baseURL string
	apiKey  string
}

// NewClient creates a new Sonarr API client.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
	}
}

// GetAllSeries returns all series from Sonarr.
func (c *Client) GetAllSeries() ([]Series, error) {
	var out []Series
	if err := c.get("/api/v3/series", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetEpisodes returns all episodes for a given series ID.
func (c *Client) GetEpisodes(seriesID int) ([]Episode, error) {
	var out []Episode
	if err := c.get(fmt.Sprintf("/api/v3/episode?seriesId=%d", seriesID), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetEpisodeFiles returns a map of episodeFileId → EpisodeFile for a given series.
func (c *Client) GetEpisodeFiles(seriesID int) (map[int]EpisodeFile, error) {
	var files []EpisodeFile
	if err := c.get(fmt.Sprintf("/api/v3/episodefile?seriesId=%d", seriesID), &files); err != nil {
		return nil, err
	}
	m := make(map[int]EpisodeFile, len(files))
	for _, f := range files {
		m[f.ID] = f
	}
	return m, nil
}

// RescanSeries triggers a Sonarr rescan for a given series ID.
func (c *Client) RescanSeries(seriesID int) error {
	body := fmt.Sprintf(`{"name":"RescanSeries","seriesId":%d}`, seriesID)
	return c.post("/api/v3/command", body)
}

// get performs a GET request and JSON-decodes into out.
func (c *Client) get(path string, out interface{}) error {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("sonarr GET %s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// post performs a POST request with a JSON body.
func (c *Client) post(path, jsonBody string) error {
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, strings.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("sonarr POST %s: HTTP %d", path, resp.StatusCode)
	}
	return nil
}

// SubtitlePath returns the Sonarr-compatible subtitle file path for a given video file.
// Naming convention: <video_basename>.<langTag>.<ext>
// zh/zh-hans/zh-bilingual → ".zh"; zh-hant → ".zh-TW"
func SubtitlePath(videoAbsPath, langTag, extWithDot string) string {
	dir := filepath.Dir(videoAbsPath)
	base := strings.TrimSuffix(filepath.Base(videoAbsPath), filepath.Ext(videoAbsPath))
	tag := sonarrLangTag(langTag)
	return filepath.Join(dir, base+"."+tag+extWithDot)
}

func sonarrLangTag(lang string) string {
	if lang == "zh-hant" {
		return "zh-TW"
	}
	return "zh"
}
