package tmdb

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"hijarr/internal/cache"
	"hijarr/internal/config"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

type TMDBInfo struct {
	Title  string
	TMDBID int
}

func FetchSeriesInfo(externalID, source string) (*TMDBInfo, error) {
	tmdbID, err := FetchTMDBIDByExternalID(externalID, source)
	if err != nil || tmdbID == 0 {
		return nil, err
	}
	return FetchSeriesInfoByID(tmdbID)
}

// FetchTMDBIDByExternalID performs a language-independent lookup from TVDB/IMDB ID to TMDB ID.
func FetchTMDBIDByExternalID(externalID, source string) (int, error) {
	if config.TMDBAPIKey == "" {
		return 0, nil
	}
	cacheKey := fmt.Sprintf("id_map_%s_%s", source, externalID)
	if val, ok := cache.Get[int](cacheKey); ok {
		return val, nil
	}

	apiURL := fmt.Sprintf("https://api.themoviedb.org/3/find/%s?api_key=%s&external_source=%s",
		externalID, config.TMDBAPIKey, source)

	resp, err := httpClient.Get(apiURL)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var data struct {
		TVResults []struct {
			ID int `json:"id"`
		} `json:"tv_results"`
		MovieResults []struct {
			ID int `json:"id"`
		} `json:"movie_results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, err
	}

	id := 0
	if len(data.TVResults) > 0 {
		id = data.TVResults[0].ID
	} else if len(data.MovieResults) > 0 {
		id = data.MovieResults[0].ID
	}

	if id != 0 {
		cache.Set(cacheKey, id)
	}
	return id, nil
}

func FetchEpisodeTitles(tmdbID, seasonNumber int) (map[int]string, error) {
	if config.TMDBAPIKey == "" {
		return map[int]string{}, nil
	}
	cacheKey := fmt.Sprintf("episodes_%d_%d_%s", tmdbID, seasonNumber, config.TargetLanguage)
	if val, ok := cache.Get[map[int]string](cacheKey); ok {
		return val, nil
	}

	apiURL := fmt.Sprintf("https://api.themoviedb.org/3/tv/%d/season/%d?api_key=%s&language=%s",
		tmdbID, seasonNumber, config.TMDBAPIKey, config.TargetLanguage)

	resp, err := httpClient.Get(apiURL)
	if err != nil {
		return map[int]string{}, err
	}
	defer resp.Body.Close()

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return map[int]string{}, err
	}

	mapping := make(map[int]string)
	if episodes, ok := data["episodes"].([]interface{}); ok {
		for _, epItem := range episodes {
			ep := epItem.(map[string]interface{})
			num := int(ep["episode_number"].(float64))
			if name, ok := ep["name"].(string); ok {
				mapping[num] = name
			}
		}
	}

	if len(mapping) > 0 {
		cache.Set(cacheKey, mapping)
	}
	return mapping, nil
}

func FetchEpisodeTitle(tmdbID, season, ep int) string {
	titles, err := FetchEpisodeTitles(tmdbID, season)
	if err != nil {
		return ""
	}
	return titles[ep]
}

// FetchSeasonName returns the localized name of a season (e.g. "貳之章" for Fire Force S2).
func FetchSeasonName(tmdbID, seasonNumber int) (string, error) {
	if config.TMDBAPIKey == "" {
		return "", nil
	}
	cacheKey := fmt.Sprintf("season_name_%d_%d_%s", tmdbID, seasonNumber, config.TargetLanguage)
	if val, ok := cache.Get[string](cacheKey); ok {
		return val, nil
	}

	apiURL := fmt.Sprintf("https://api.themoviedb.org/3/tv/%d/season/%d?api_key=%s&language=%s",
		tmdbID, seasonNumber, config.TMDBAPIKey, config.TargetLanguage)

	resp, err := httpClient.Get(apiURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var data struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}

	if data.Name != "" {
		cache.Set(cacheKey, data.Name)
	}
	return data.Name, nil
}

// searchTV is the private HTTP layer for TMDB TV search.
// Results (up to 5) are cached in tmdb_cache under "search_{query}_{lang}".
func searchTV(query string) ([]TMDBInfo, error) {
	cacheKey := fmt.Sprintf("search_%s_%s", query, config.TargetLanguage)
	if val, ok := cache.Get[[]TMDBInfo](cacheKey); ok {
		return val, nil
	}

	apiURL := fmt.Sprintf("https://api.themoviedb.org/3/search/tv?api_key=%s&query=%s&language=%s&page=1",
		config.TMDBAPIKey, url.QueryEscape(query), config.TargetLanguage)

	resp, err := httpClient.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data struct {
		Results []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var infos []TMDBInfo
	for i, r := range data.Results {
		if i >= 5 {
			break
		}
		if r.Name != "" {
			infos = append(infos, TMDBInfo{Title: r.Name, TMDBID: r.ID})
		}
	}
	if len(infos) > 0 {
		cache.Set(cacheKey, infos)
	}
	return infos, nil
}

// FetchSeriesInfoByQuery returns the best-matching TMDB TV series for the given query.
func FetchSeriesInfoByQuery(query string) (*TMDBInfo, error) {
	if config.TMDBAPIKey == "" {
		return nil, nil
	}
	results, err := searchTV(query)
	if err != nil || len(results) == 0 {
		return nil, err
	}
	return &results[0], nil
}

// FetchSeriesSearchResults returns up to 5 TMDB TV series matches for autocomplete.
func FetchSeriesSearchResults(query string) ([]TMDBInfo, error) {
	if config.TMDBAPIKey == "" {
		return nil, nil
	}
	return searchTV(query)
}

// FetchSeriesInfoByID retrieves primary series information from TMDB.
func FetchSeriesInfoByID(tmdbID int) (*TMDBInfo, error) {
	if config.TMDBAPIKey == "" {
		return nil, nil
	}
	cacheKey := fmt.Sprintf("info_id_%d_%s", tmdbID, config.TargetLanguage)
	if val, ok := cache.Get[*TMDBInfo](cacheKey); ok {
		return val, nil
	}

	apiURL := fmt.Sprintf("https://api.themoviedb.org/3/tv/%d?api_key=%s&language=%s",
		tmdbID, config.TMDBAPIKey, config.TargetLanguage)

	resp, err := httpClient.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data struct {
		Name string `json:"name"`
		ID   int    `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	info := &TMDBInfo{Title: data.Name, TMDBID: data.ID}
	cache.Set(cacheKey, info)
	return info, nil
}

// FetchTVDBID returns the TVDB ID for a TMDB series, or 0 if not found.
func FetchTVDBID(tmdbID int) (int, error) {
	if config.TMDBAPIKey == "" {
		return 0, nil
	}
	cacheKey := fmt.Sprintf("tvdb_id_%d", tmdbID)
	if val, ok := cache.Get[int](cacheKey); ok {
		return val, nil
	}

	apiURL := fmt.Sprintf("https://api.themoviedb.org/3/tv/%d/external_ids?api_key=%s",
		tmdbID, config.TMDBAPIKey)

	resp, err := httpClient.Get(apiURL)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var data struct {
		TvdbID int `json:"tvdb_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, err
	}

	cache.Set(cacheKey, data.TvdbID)
	return data.TvdbID, nil
}

func FetchChineseTitleByQuery(query string) (string, error) {
	info, err := FetchSeriesInfoByQuery(query)
	if err != nil || info == nil {
		return "", err
	}
	return info.Title, nil
}

// FetchSeriesAliases returns a list of all known titles (alternative and translated)
// for a given TMDB series ID.
func FetchSeriesAliases(tmdbID int) ([]string, error) {
	if config.TMDBAPIKey == "" {
		return nil, nil
	}
	cacheKey := fmt.Sprintf("aliases_%d", tmdbID)
	if val, ok := cache.Get[[]string](cacheKey); ok {
		return val, nil
	}

	titles := make(map[string]bool)

	// 1. Fetch Alternative Titles
	altURL := fmt.Sprintf("https://api.themoviedb.org/3/tv/%d/alternative_titles?api_key=%s",
		tmdbID, config.TMDBAPIKey)
	resp, err := httpClient.Get(altURL)
	if err == nil {
		var data struct {
			Results []struct {
				Title string `json:"title"`
			} `json:"results"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
			for _, r := range data.Results {
				titles[r.Title] = true
			}
		}
		resp.Body.Close()
	}

	// 2. Fetch Translations
	transURL := fmt.Sprintf("https://api.themoviedb.org/3/tv/%d/translations?api_key=%s",
		tmdbID, config.TMDBAPIKey)
	resp, err = httpClient.Get(transURL)
	if err == nil {
		var data struct {
			Translations []struct {
				Data struct {
					Name string `json:"name"`
				} `json:"data"`
			} `json:"translations"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
			for _, t := range data.Translations {
				if t.Data.Name != "" {
					titles[t.Data.Name] = true
				}
			}
		}
		resp.Body.Close()
	}

	var result []string
	for t := range titles {
		result = append(result, t)
	}

	if len(result) > 0 {
		cache.Set(cacheKey, result)
	}
	return result, nil
}

// FetchSeasonCount returns the number of seasons for a TMDB series.
// Returns -1 if the information cannot be retrieved.
func FetchSeasonCount(tmdbID int) int {
	if config.TMDBAPIKey == "" || tmdbID == 0 {
		return -1
	}
	cacheKey := fmt.Sprintf("season_count_%d", tmdbID)
	if val, ok := cache.Get[int](cacheKey); ok {
		return val
	}
	apiURL := fmt.Sprintf("https://api.themoviedb.org/3/tv/%d?api_key=%s", tmdbID, config.TMDBAPIKey)
	resp, err := httpClient.Get(apiURL)
	if err != nil {
		return -1
	}
	defer resp.Body.Close()
	var data struct {
		NumberOfSeasons int `json:"number_of_seasons"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return -1
	}
	cache.Set(cacheKey, data.NumberOfSeasons)
	return data.NumberOfSeasons
}
