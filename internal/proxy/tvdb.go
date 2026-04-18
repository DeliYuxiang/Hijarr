package proxy

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"

	"hijarr/internal/config"
	"hijarr/internal/tmdb"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var tvdbClient = &http.Client{
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
}

func TVDBMitmProxy(c *gin.Context) {
	path := c.Param("path")
	if strings.HasPrefix(path, "/api") {
		TorznabProxy(c)
		return
	}

	path = strings.TrimPrefix(path, "/")

	parts := strings.Split(path, "/")
	langModified := false

	if len(parts) >= 3 {
		for _, i := range []int{len(parts) - 1, len(parts) - 2} {
			if i >= 0 && len(parts[i]) == 3 && isAlpha(parts[i]) {
				if parts[i] != config.TVDBLanguage {
					log.Debug("🌐 [语言劫持] %s -> %s\n", parts[i], config.TVDBLanguage)
					parts[i] = config.TVDBLanguage
					langModified = true
				}
				break
			}
		}
	}

	newPath := path
	if langModified {
		newPath = strings.Join(parts, "/")
	}

	requestHost := c.Request.Host
	if requestHost == "" {
		requestHost = "api.thetvdb.com"
	}

	targetHost := requestHost
	if strings.Contains(requestHost, "sonarr.tv") {
		targetHost = "skyhook.sonarr.tv"
	} else if !strings.Contains(requestHost, "thetvdb.com") {
		targetHost = "api.thetvdb.com"
	}

	realURL := fmt.Sprintf("https://%s/%s", targetHost, newPath)
	if c.Request.URL.RawQuery != "" {
		realURL += "?" + c.Request.URL.RawQuery
	}

	var reqBody []byte
	if c.Request.Body != nil {
		reqBody, _ = io.ReadAll(c.Request.Body)
	}
	req, _ := http.NewRequest(c.Request.Method, realURL, bytes.NewReader(reqBody))

	forwardHeaders(req, c.Request)
	req.Host = targetHost

	resp, err := tvdbClient.Do(req)
	if err != nil {
		log.Warn("❌ TVDB Proxy 转发失败: %v\n", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	contentType := resp.Header.Get("Content-Type")

	if strings.Contains(contentType, "application/json") && resp.StatusCode == 200 {
		jsonStr := string(respBody)
		modified := false

		if strings.Contains(path, "v1/tvdb/shows/") {
			tvdbID := gjson.Get(jsonStr, "tvdbId")
			if tvdbID.Exists() {
				tmdbInfo, _ := tmdb.FetchSeriesInfo(tvdbID.String(), "tvdb_id")
				if tmdbInfo != nil {
					jsonStr, _ = sjson.Set(jsonStr, "title", tmdbInfo.Title)

					episodes := gjson.Get(jsonStr, "episodes")
					if episodes.IsArray() {
						seasons := make(map[int]bool)
						episodes.ForEach(func(_, ep gjson.Result) bool {
							sNum := ep.Get("seasonNumber").Int()
							seasons[int(sNum)] = true
							return true
						})

						epMapping := make(map[int]map[int]string)
						for s := range seasons {
							mapping, _ := tmdb.FetchEpisodeTitles(tmdbInfo.TMDBID, s)
							epMapping[s] = mapping
						}

						episodes.ForEach(func(index, ep gjson.Result) bool {
							sNum := int(ep.Get("seasonNumber").Int())
							eNum := int(ep.Get("episodeNumber").Int())
							if m, ok := epMapping[sNum]; ok {
								if title, exists := m[eNum]; exists {
									jsonPath := fmt.Sprintf("episodes.%d.title", index.Int())
									jsonStr, _ = sjson.Set(jsonStr, jsonPath, title)
								}
							}
							return true
						})
					}
					modified = true
					log.Debug("💀 [Skyhook 详情] ID: %s 已翻译\n", tvdbID.String())
				}
			}
		} else if strings.Contains(path, "v1/tvdb/search/") {
			if gjson.Valid(jsonStr) {
				gjson.Parse(jsonStr).ForEach(func(key, item gjson.Result) bool {
					tvdbID := item.Get("tvdbId")
					if tvdbID.Exists() {
						tmdbInfo, _ := tmdb.FetchSeriesInfo(tvdbID.String(), "tvdb_id")
						if tmdbInfo != nil {
							jsonPath := fmt.Sprintf("%s.title", key.String())
							jsonStr, _ = sjson.Set(jsonStr, jsonPath, tmdbInfo.Title)
							modified = true
						}
					}
					return true
				})
				if modified {
					log.Debug("💀 [Skyhook 搜索] 结果已批量翻译\n")
				}
			}
		} else if strings.HasPrefix(path, "v4/series/") && gjson.Get(jsonStr, "data").Exists() {
			seriesData := gjson.Get(jsonStr, "data.series")
			if !seriesData.Exists() {
				seriesData = gjson.Get(jsonStr, "data")
			}

			tvdbID := seriesData.Get("id")
			if tvdbID.Exists() {
				tmdbInfo, _ := tmdb.FetchSeriesInfo(tvdbID.String(), "tvdb_id")
				if tmdbInfo != nil {
					chineseTitle := tmdbInfo.Title

					if seriesData.Get("name").Exists() {
						jpath := "data.series.name"
						if !gjson.Get(jsonStr, "data.series").Exists() {
							jpath = "data.name"
						}
						jsonStr, _ = sjson.Set(jsonStr, jpath, chineseTitle)
						modified = true
					}

					if seriesData.Get("translations.nameTranslations").Exists() {
						jpath := "data.series.translations.nameTranslations"
						if !gjson.Get(jsonStr, "data.series").Exists() {
							jpath = "data.translations.nameTranslations"
						}
						jsonStr, _ = sjson.Set(jsonStr, jpath, []map[string]string{
							{"name": chineseTitle, "language": config.TVDBLanguage},
						})
						modified = true
					}

					episodes := gjson.Get(jsonStr, "data.episodes")
					if episodes.IsArray() {
						seasons := make(map[int]bool)
						episodes.ForEach(func(_, ep gjson.Result) bool {
							if ep.Get("seasonNumber").Exists() {
								sNum := ep.Get("seasonNumber").Int()
								seasons[int(sNum)] = true
							}
							return true
						})

						epMapping := make(map[int]map[int]string)
						for s := range seasons {
							mapping, _ := tmdb.FetchEpisodeTitles(tmdbInfo.TMDBID, s)
							epMapping[s] = mapping
						}

						episodes.ForEach(func(index, ep gjson.Result) bool {
							sNum := int(ep.Get("seasonNumber").Int())
							eNum := int(ep.Get("number").Int())
							if m, ok := epMapping[sNum]; ok {
								if title, exists := m[eNum]; exists {
									jsonPath := fmt.Sprintf("data.episodes.%d.name", index.Int())
									jsonStr, _ = sjson.Set(jsonStr, jsonPath, title)
									modified = true
								}
							}
							return true
						})
					}

					if modified {
						log.Debug("💀 [TVDB 劫持] ID: %s 数据已篡改 (TMDB ID: %d)\n", tvdbID.String(), tmdbInfo.TMDBID)
					}
				}
			}
		} else if strings.HasPrefix(path, "v4/search") && gjson.Get(jsonStr, "data").IsArray() {
			gjson.Get(jsonStr, "data").ForEach(func(index, item gjson.Result) bool {
				tvdbID := item.Get("tvdb_id")
				if tvdbID.Exists() {
					tmdbInfo, _ := tmdb.FetchSeriesInfo(tvdbID.String(), "tvdb_id")
					if tmdbInfo != nil {
						jsonPath := fmt.Sprintf("data.%d.name", index.Int())
						jsonStr, _ = sjson.Set(jsonStr, jsonPath, tmdbInfo.Title)
						modified = true
					}
				}
				return true
			})
			if modified {
				log.Debug("💀 [TVDB 劫持] 搜索结果已批量篡改\n")
			}
		}

		if modified {
			c.Data(200, "application/json", []byte(jsonStr))
			return
		}
	}

	copyResponseHeaders(c, resp)
	c.Data(resp.StatusCode, contentType, respBody)
}

func isAlpha(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}
