package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"


	"hijarr/internal/config"

	"github.com/gin-gonic/gin"
)

// Reusable mockTransport for deterministic testing
type mockTransport struct {
	RoundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.RoundTripFunc(req)
}

func setupTestRouterFixed() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.Default()

	r.Any("/*path", func(c *gin.Context) {
		path := c.Param("path")

		if strings.HasPrefix(path, "/api") {
			TorznabProxy(c)
			return
		}
		TVDBMitmProxy(c)
	})
	return r
}

func TestMain(m *testing.M) {
	config.TMDBAPIKey = "fake_tmdb_key"
	config.ProwlarrAPIKey = "fake_prowlarr_key"
	config.TargetLanguage = "zh-CN"
	config.CacheDBPath = ":memory:" // use in-memory SQLite for tests
	os.Exit(m.Run())
}


func readTestData(t *testing.T, filePath string) []string {
	file, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("Failed to open test data file: %v", err)
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func buildMockTransport() *mockTransport {
	return &mockTransport{
		RoundTripFunc: func(req *http.Request) (*http.Response, error) {
			// Mock TMDB /find
			if strings.Contains(req.URL.Host, "api.themoviedb.org") && strings.Contains(req.URL.Path, "/find/") {
				parts := strings.Split(req.URL.Path, "/")
				tvdbID := parts[len(parts)-1]
				resp := map[string]interface{}{
					"tv_results": []map[string]interface{}{{"id": 999, "name": "Series_" + tvdbID}},
				}
				b, _ := json.Marshal(resp)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(b)), Header: make(http.Header)}, nil
			}

			// Mock TMDB /search/tv
			if strings.Contains(req.URL.Host, "api.themoviedb.org") && strings.Contains(req.URL.Path, "/search/tv") {
				q := req.URL.Query().Get("query")
				resp := map[string]interface{}{
					"results": []map[string]interface{}{{"id": 999, "name": "Translated_" + q}},
				}
				b, _ := json.Marshal(resp)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(b)), Header: make(http.Header)}, nil
			}

			// Mock TMDB series detail
			if strings.Contains(req.URL.Host, "api.themoviedb.org") && regexp.MustCompile(`/tv/\d+`).MatchString(req.URL.Path) {
				resp := map[string]interface{}{
					"id":   999,
					"name": "Translated_Series",
				}
				b, _ := json.Marshal(resp)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(b)), Header: make(http.Header)}, nil
			}

			// Mock Prowlarr RSS

			if strings.Contains(req.URL.Host, "prowlarr:9696") {
				xmlStr := `<?xml version="1.0" encoding="UTF-8"?><rss><channel><item><title>Result</title><guid>magnet:?xt=urn:btih:1</guid></item></channel></rss>`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(xmlStr)), Header: make(http.Header)}, nil
			}

			return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("Not Found"))}, nil
		},
	}
}


func TestProxyLifecycle(t *testing.T) {
	tvdbIDs := readTestData(t, "../../test_data/tvdb_ids.txt")

	mockTrans := buildMockTransport()

	originalTransport := http.DefaultTransport
	prowlarrClient.Transport = mockTrans
	http.DefaultTransport = mockTrans
	defer func() {
		http.DefaultTransport = originalTransport
		prowlarrClient.Transport = originalTransport
	}()

	r := setupTestRouterFixed()

	for _, id := range tvdbIDs {

		t.Run("Torznab_ID_"+id, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/api?t=tvsearch&tvdbid="+id+"&season=1", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != 200 {
				t.Fatalf("Expected 200 for Torznab search with ID %s, got %d", id, w.Code)
			}
		})
	}
}

func TestMultiLanguageQueryInterception(t *testing.T) {

	queries := readTestData(t, "../../test_data/test_queries.txt")

	mockTrans := &mockTransport{
		RoundTripFunc: func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Host, "api.themoviedb.org") && strings.Contains(req.URL.Path, "/search/tv") {
				q := req.URL.Query().Get("query")
				resp := map[string]interface{}{
					"results": []map[string]interface{}{{"id": 111, "name": "统一中文剧名"}},
				}
				fmt.Printf("Mock TMDB: Received query '%s', returning '统一中文剧名'\n", q)
				b, _ := json.Marshal(resp)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(b)), Header: make(http.Header)}, nil
			}

			if strings.Contains(req.URL.Host, "prowlarr:9696") {
				xmlStr := `<?xml version="1.0" encoding="UTF-8"?><rss><channel><item><title>Found</title><guid>magnet:?xt=1</guid></item></channel></rss>`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(xmlStr)), Header: make(http.Header)}, nil
			}

			if strings.Contains(req.URL.Host, "api.assrt.net") && strings.Contains(req.URL.Path, "/v1/sub/search") {
				q := req.URL.Query().Get("q")
				// Use a hash of the query as the ID so different queries get different IDs
				// and the aggregator deduplication doesn't drop the translated result.
				var idNum int
				for _, c := range q {
					idNum = (idNum*31 + int(c)) & 0xFFFF
				}
				filename := "Mock.S01E01.srt"
				if strings.Contains(q, "统一中文剧名") {
					filename = "Correct.Translated.S01E01.Chinese.srt"
				}
				resp := map[string]interface{}{
					"status": 0,
					"sub": map[string]interface{}{
						"subs": []map[string]interface{}{{"id": idNum, "native_name": filename, "videoname": filename}},
					},
				}
				b, _ := json.Marshal(resp)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(b)), Header: make(http.Header)}, nil
			}

			// Mock /v1/sub/detail — return a fake download URL pointing to a .srt file
			if strings.Contains(req.URL.Host, "api.assrt.net") && strings.Contains(req.URL.Path, "/v1/sub/detail") {
				resp := map[string]interface{}{
					"status": 0,
					"sub": map[string]interface{}{
						"subs": []map[string]interface{}{
							{"url": []map[string]interface{}{{"url": "http://file.assrt.net/mock/Correct.Translated.S01E01.Chinese.srt"}}},
						},
					},
				}
				b, _ := json.Marshal(resp)
				h := make(http.Header)
				h.Set("Content-Type", "application/json")
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(b)), Header: h}, nil
			}

			// Mock subtitle file download (file.assrt.net)
			if strings.Contains(req.URL.Host, "file.assrt.net") {
				srtContent := "1\n00:00:01,000 --> 00:00:02,000\nTest subtitle\n"
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(srtContent)), Header: make(http.Header)}, nil
			}

			return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("Not Found"))}, nil
		},
	}

	originalTransport := http.DefaultTransport
	prowlarrClient.Transport = mockTrans
	http.DefaultTransport = mockTrans
	defer func() {
		http.DefaultTransport = originalTransport
		prowlarrClient.Transport = originalTransport
	}()

	r := setupTestRouterFixed()

	for _, q := range queries {
		t.Run("Torznab_Q_"+q, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/api?t=tvsearch&q="+q+"&season=1&ep=1", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != 200 {
				t.Errorf("Torznab failed for language query '%s': %d", q, w.Code)
			}
		})
	}
}

