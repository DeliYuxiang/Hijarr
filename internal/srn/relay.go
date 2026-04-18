package srn

import (
	"bytes"
	"crypto/ed25519"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"hijarr/internal/config"
	"hijarr/internal/logger"
)

// ErrPermanentUpload is returned when an upload failure cannot be fixed by retrying
// (HTTP 4xx other than 429 Too Many Requests).
type ErrPermanentUpload struct {
	StatusCode int
	Body       string
}

func (e *ErrPermanentUpload) Error() string {
	return fmt.Sprintf("status %d: %s", e.StatusCode, e.Body)
}

var relayLog = logger.For("srn-client")
var relayClient = &http.Client{Timeout: 30 * time.Second}


type challengeInfo struct {
	Salt string `json:"salt"`
	K    int    `json:"k"`
	VIP  bool   `json:"vip"`
}

type nonceInfo struct {
	Nonce string
	Salt  string
}

var (
	// nonceCache maps relayURL -> valid nonce (valid ~1 minute until salt rotates)
	nonceCache   = make(map[string]*nonceInfo)
	nonceCacheMu sync.RWMutex
)

func getRelayBase(urlStr string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s://%s", u.Scheme, u.Host)
}

func fetchChallenge(relayURL string, pubkey string) (*challengeInfo, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/v1/challenge", relayURL), nil)
	if err != nil {
		return nil, err
	}
	if pubkey != "" {
		req.Header.Set("X-SRN-PubKey", pubkey)
	}
	resp, err := relayClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("challenge status %d", resp.StatusCode)
	}
	var ci challengeInfo
	if err := json.NewDecoder(resp.Body).Decode(&ci); err != nil {
		return nil, err
	}
	return &ci, nil
}

func mineNonce(salt, pubkey string, k int) string {
	if k <= 0 {
		return "0"
	}
	prefix := strings.Repeat("0", k)
	for i := 0; ; i++ {
		nonce := strconv.Itoa(i)
		h := sha256.New()
		h.Write([]byte(salt))
		h.Write([]byte(pubkey))
		h.Write([]byte(nonce))
		hash := hex.EncodeToString(h.Sum(nil))
		if strings.HasPrefix(hash, prefix) {
			return nonce
		}
		if i > 5000000 { // Safety break
			return "0"
		}
	}
}

// seedNonceFromAuthError parses the challenge embedded in a 401/403 response body,
// mines a fresh nonce inline, and writes it to nonceCache — saving the extra
// GET /v1/challenge round trip on auth failure.
// Falls back to clearing the cache if the body cannot be parsed; ensureNonce
// will then fetch the challenge separately on the next call.
func seedNonceFromAuthError(relayURL, pubkey string, body []byte) {
	var errResp struct {
		Challenge *challengeInfo `json:"challenge"`
	}
	if err := json.Unmarshal(body, &errResp); err != nil || errResp.Challenge == nil {
		nonceCacheMu.Lock()
		delete(nonceCache, relayURL)
		nonceCacheMu.Unlock()
		return
	}
	ci := errResp.Challenge
	nonce := mineNonce(ci.Salt, pubkey, ci.K)
	nonceCacheMu.Lock()
	nonceCache[relayURL] = &nonceInfo{Nonce: nonce, Salt: ci.Salt}
	nonceCacheMu.Unlock()
}

func ensureNonce(relayURL, pubkey string) string {
	nonceCacheMu.RLock()
	n, ok := nonceCache[relayURL]
	nonceCacheMu.RUnlock()
	if ok {
		return n.Nonce
	}

	ci, err := fetchChallenge(relayURL, pubkey)
	if err != nil {
		relayLog.Error("❌ [SRN] 获取挑战失败 (%s): %v", relayURL, err)
		return "0"
	}

	nonce := mineNonce(ci.Salt, pubkey, ci.K)
	nonceCacheMu.Lock()
	nonceCache[relayURL] = &nonceInfo{Nonce: nonce, Salt: ci.Salt}
	nonceCacheMu.Unlock()
	return nonce
}

// nodeGET holds the pre-computed GET signing credential for this node.
// Canonical message for all GET requests = pubKeyHex (self-certification, same pattern
// as tmdb.ts routes). The signature is stable per key — computed once in SetNodeKey.
var (
	nodeGETMu   sync.RWMutex
	nodePubHex  string
	nodePrivKey ed25519.PrivateKey
	nodeGETSig  string // hex-encoded Ed25519 signature over UTF-8(pubKeyHex)
)

// SetNodeKey registers the node's Ed25519 identity key for signing outbound GET requests.
// Pre-computes the self-certification signature (canonicalMsg = pubKeyHex) once so it
// can be attached to every request without re-signing.
func SetNodeKey(priv ed25519.PrivateKey) {
	if priv == nil {
		return
	}
	pub := priv.Public().(ed25519.PublicKey)
	pubHex := hex.EncodeToString(pub)
	sig := ed25519.Sign(priv, []byte(pubHex))
	nodeGETMu.Lock()
	nodePubHex = pubHex
	nodePrivKey = priv
	nodeGETSig = hex.EncodeToString(sig)
	nodeGETMu.Unlock()
}

// addGetAuth attaches X-SRN-PubKey, X-SRN-Nonce and X-SRN-Signature headers to an outbound GET request.
// No-op when SetNodeKey has not been called.
func addGetAuth(req *http.Request) {
	nodeGETMu.RLock()
	pub, priv, sig := nodePubHex, nodePrivKey, nodeGETSig
	nodeGETMu.RUnlock()
	if pub == "" {
		return
	}

	relayURL := getRelayBase(req.URL.String())
	nonce := ensureNonce(relayURL, pub)

	req.Header.Set("X-SRN-PubKey", pub)
	req.Header.Set("X-SRN-Nonce", nonce)

	// Check if this is a download request (ends with /content)
	if strings.HasSuffix(req.URL.Path, "/content") {
		// Download interface uses current minute string as canonical message
		minute := strconv.FormatInt(time.Now().Unix()/60, 10)
		sig = hex.EncodeToString(ed25519.Sign(priv, []byte(minute)))
	}

	req.Header.Set("X-SRN-Signature", sig)
}

// doSignedGet issues a signed GET request. On 401/403, it refreshes PoW nonce
// and retries the request once.
func doSignedGet(urlStr string) (*http.Response, error) {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	addGetAuth(req)
	resp, err := relayClient.Do(req)
	if err != nil {
		return nil, err
	}

	// 400 有两种来源：
	//   a) relay 将过期 nonce 报为 400（非 401）——body 含 "challenge" 字段
	//   b) 真正的请求参数错误——body 不含 challenge
	// 对 400/401/403 统一尝试 nonce 刷新；若 body 无 challenge 则 seedNonce 仅清缓存，
	// 重试后仍失败会在调用方被记录。
	if resp.StatusCode == 400 || resp.StatusCode == 401 || resp.StatusCode == 403 {
		relayURL := getRelayBase(urlStr)
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		nodeGETMu.RLock()
		pub := nodePubHex
		nodeGETMu.RUnlock()

		relayLog.Warn("🔑 [SRN] GET %s → %d, body: %s, 刷新 PoW 并重试",
			urlStr, resp.StatusCode, truncateBody(b, 200))
		seedNonceFromAuthError(relayURL, pub, b)

		// Retry once with freshly mined nonce
		req, _ = http.NewRequest("GET", urlStr, nil)
		addGetAuth(req)
		return relayClient.Do(req)
	}
	return resp, nil
}

// truncateBody returns up to maxLen bytes of b as a string for log messages.
func truncateBody(b []byte, maxLen int) string {
	if len(b) <= maxLen {
		return string(b)
	}
	return string(b[:maxLen]) + "…"
}

// publishPayload is the wire format sent to the SRN relay as the `event` form field.
type publishPayload struct {
	ID         string     `json:"id"`
	PubKey     string     `json:"pubkey"`
	Kind       int        `json:"kind"`
	Tags       [][]string `json:"tags"`
	ContentMD5 string     `json:"content_md5"`
	Filename   string     `json:"filename,omitempty"`
	// Flat metadata fields required by TS relay for indexing/filtering
	TmdbID     string `json:"tmdb_id,omitempty"`
	SeasonNum  int    `json:"season_num,omitempty"`
	EpisodeNum int    `json:"episode_num,omitempty"`
	Language   string `json:"language,omitempty"`
	ArchiveMD5 string `json:"archive_md5,omitempty"`
	SourceType string `json:"source_type,omitempty"`
	SourceURI  string `json:"source_uri,omitempty"`
}

// PublishToNetwork signs and broadcasts an event to all configured SRN relays.
func PublishToNetwork(ev *Event, data []byte, privKey ed25519.PrivateKey) error {
	if len(config.SRNRelayURLs) == 0 {
		return fmt.Errorf("no relays configured")
	}

	if data != nil && ev.ContentMD5 == "" {
		ev.ContentMD5 = fmt.Sprintf("%x", md5.Sum(data))
	}

	ev.CreatedAt = time.Now().Unix()
	ev.PubKey = hex.EncodeToString(privKey.Public().(ed25519.PublicKey))
	ev.ID = ev.ComputeID()

	// Build payload, extracting flat metadata from tags
	payload := &publishPayload{
		ID:         ev.ID,
		PubKey:     ev.PubKey,
		Kind:       ev.Kind,
		Tags:       ev.Tags,
		ContentMD5: ev.ContentMD5,
		Filename:   ev.Filename,
		TmdbID:     ev.GetTag("tmdb"),
		Language:   ev.GetTag("language"),
		ArchiveMD5: ev.GetTag("archive_md5"),
		SourceType: ev.GetTag("source_type"),
		SourceURI:  ev.GetTag("source_uri"),
	}
	if s, err := strconv.Atoi(ev.GetTag("s")); err == nil {
		payload.SeasonNum = s
	}
	if e, err := strconv.Atoi(ev.GetTag("e")); err == nil {
		payload.EpisodeNum = e
	}

	// Marshal full payload for the wire (relay needs flat metadata for SQL indexing).
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal failed: %v", err)
	}

	// Sign the canonical tuple — NOT the full payloadJSON.
	// Canonical form: JSON([pubkey, kind, canonical_tags, content_md5])
	// This must match the relay worker's verification logic in events.ts.
	canonicalMsg, err := canonicalJSON([]interface{}{ev.PubKey, ev.Kind, canonicalTagsFor(ev.Tags), ev.ContentMD5})
	if err != nil {
		return fmt.Errorf("canonical marshal failed: %v", err)
	}
	sig := ed25519.Sign(privKey, canonicalMsg)
	sigHex := hex.EncodeToString(sig)
	ev.Sig = sigHex

	var mu sync.Mutex
	var errs []error
	successes := 0

	var wg sync.WaitGroup
	for _, rURL := range config.SRNRelayURLs {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			if err := pushToOneRelay(u, ev.PubKey, sigHex, payloadJSON, ev.Filename, data); err != nil {
				relayLog.Error("❌ [SRN] Push to %s failed: %v", u, err)
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			} else {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}(rURL)
	}
	wg.Wait()

	if successes > 0 {
		return nil
	}
	if len(errs) == 0 {
		return nil
	}

	// All relays failed. Return permanent error if all were permanent; otherwise transient.
	allPermanent := true
	for _, err := range errs {
		var pe *ErrPermanentUpload
		if !errors.As(err, &pe) {
			allPermanent = false
			break
		}
	}
	if allPermanent {
		return errs[0] // *ErrPermanentUpload
	}
	return fmt.Errorf("%d relay(s) failed: %w", len(errs), errs[0])
}

// RetractEvent publishes a Kind 1002 event to retract a previously published event.
func RetractEvent(targetID, reason string, privKey ed25519.PrivateKey) error {
	pubkey := hex.EncodeToString(privKey.Public().(ed25519.PublicKey))
	ev := NewRetractEvent(pubkey, targetID, reason)
	return PublishToNetwork(ev, nil, privKey)
}

// ReplaceEvent publishes a Kind 1003 event to supersede a previous event.
func ReplaceEvent(prevID string, tags [][]string, data []byte, filename string, privKey ed25519.PrivateKey) error {
	pubkey := hex.EncodeToString(privKey.Public().(ed25519.PublicKey))
	contentMD5 := fmt.Sprintf("%x", md5.Sum(data))
	ev := NewReplaceEvent(pubkey, prevID, tags, contentMD5)
	ev.Filename = filename
	return PublishToNetwork(ev, data, privKey)
}

// QueryNetwork searches all remote relays for subtitles and returns filtered results.
func QueryNetwork(tmdbID, lang string, season, ep int) []Event {
	if len(config.SRNRelayURLs) == 0 {
		return nil
	}

	// Relay already excludes deactivated events server-side via event_lifecycle,
	// so no need to fetch Kind 1002 separately.
	var mu sync.Mutex
	var allEvents []Event
	var wg sync.WaitGroup

	for _, rURL := range config.SRNRelayURLs {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			events, err := queryOne(u, tmdbID, lang, season, ep)
			if err != nil {
				relayLog.Error("❌ [SRN] Query %s failed: %v", u, err)
				return
			}
			mu.Lock()
			mergeEvents(&allEvents, events)
			mu.Unlock()
		}(rURL)
	}
	wg.Wait()
	return allEvents
}


// QueryNetworkForLangs 按语言列表向所有 relay 查询字幕，结果按 ID 去重合并。
// 若 langs 为空，回退到 "zh"（前缀匹配所有中文变体）。
// 单语言时直接委托 QueryNetwork，无额外开销。
func QueryNetworkForLangs(tmdbID string, langs []config.SubtitleLanguage, season, ep int) []Event {
	if len(langs) == 0 {
		return QueryNetwork(tmdbID, string(config.LangZH), season, ep)
	}
	if len(langs) == 1 {
		return QueryNetwork(tmdbID, string(langs[0]), season, ep)
	}
	var all []Event
	for _, lang := range langs {
		evs := QueryNetwork(tmdbID, string(lang), season, ep)
		mergeEvents(&all, evs)
	}
	return all
}

func queryOne(relayURL, tmdbID, lang string, season, ep int) ([]Event, error) {
	params := url.Values{}
	params.Set("kind", "1001") // Only fetch subtitle events by default
	if tmdbID != "" {
		params.Set("tmdb", tmdbID)
	}
	if lang != "" {
		params.Set("language", lang)
	}
	// season=0 is valid (OVA/specials); always include season when ep is present
	// because the relay requires season whenever ep is specified (Zod validation).
	if ep > 0 {
		params.Set("season", strconv.Itoa(season))
		params.Set("ep", strconv.Itoa(ep))
	} else if season > 0 {
		params.Set("season", strconv.Itoa(season))
	}

	resp, err := doSignedGet(fmt.Sprintf("%s/v1/events?%s", relayURL, params.Encode()))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, truncateBody(b, 300))
	}

	var res struct {
		Events []Event `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return res.Events, nil
}

// RelayIdentity represents the metadata of an SRN relay.
type RelayIdentity struct {
	PubKey      string `json:"pubkey"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// QueryRelayIdentity fetches the identity of a relay.
func QueryRelayIdentity(relayURL string) (*RelayIdentity, error) {
	resp, err := relayClient.Get(fmt.Sprintf("%s/v1/identity", relayURL))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var identity RelayIdentity
	if err := json.NewDecoder(resp.Body).Decode(&identity); err != nil {
		return nil, err
	}
	return &identity, nil
}

func pushToOneRelay(relayURL, pubKeyHex, sigHex string, payloadJSON []byte, filename string, data []byte) error {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	_ = writer.WriteField("event", string(payloadJSON))

	part, _ := writer.CreateFormFile("file", filename)
	if data == nil {
		data = []byte{} // Ensure we send an empty file field for non-content events
	}
	_, _ = io.Copy(part, bytes.NewReader(data))
	writer.Close()

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/v1/events", relayURL), body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-SRN-PubKey", pubKeyHex)
	req.Header.Set("X-SRN-Nonce", ensureNonce(relayURL, pubKeyHex))
	req.Header.Set("X-SRN-Signature", sigHex)

	resp, err := relayClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			// Auth failure: nonce may have expired (salt rotates per minute).
			// Seed cache from the challenge embedded in the response so the queue
			// worker's next retry can mine immediately without an extra /v1/challenge RTT.
			seedNonceFromAuthError(relayURL, pubKeyHex, b)
			return fmt.Errorf("auth %d: %s", resp.StatusCode, string(b))
		}
		// Other 4xx (except 429 Too Many Requests) are permanent: retrying won't help.
		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
			return &ErrPermanentUpload{StatusCode: resp.StatusCode, Body: string(b)}
		}
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func mergeEvents(target *[]Event, source []Event) {
	seen := make(map[string]struct{}, len(*target))
	for _, t := range *target {
		seen[t.ID] = struct{}{}
	}
	for _, s := range source {
		if _, ok := seen[s.ID]; !ok {
			*target = append(*target, s)
		}
	}
}

// DownloadFromRelays fetches subtitle content by event ID from remote relays.
func DownloadFromRelays(id string) ([]byte, error) {
	for _, u := range config.SRNRelayURLs {
		resp, err := doSignedGet(fmt.Sprintf("%s/v1/events/%s/content", u, id))
		if err != nil {
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return io.ReadAll(resp.Body)
		}
	}
	return nil, fmt.Errorf("not found on any relay")
}
