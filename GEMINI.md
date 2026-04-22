# Hijarr - Torznab Proxy & Metadata MITM for Chinese Anime Media

Hijarr is a Go **Smart Client** proxy for Sonarr/Prowlarr, purpose-built for Chinese-language anime environments. It intercepts metadata requests to `skyhook.sonarr.tv` and `api.thetvdb.com`, rewrites titles and episode names with TMDB Chinese translations, translates Torznab search queries from English to Chinese, and auto-syncs subtitles from the **SRN (Subtitle Relay Network)** directly to the Sonarr media library — replacing Bazarr.

## Architecture & Core Logic

### Entry Point

`cmd/hijarr/main.go` — initialises logger, SRN node identity (Ed25519 key priority: `SRN_PRIV_KEY` env > SQLite `global_state` > auto-generate with WARN), runs maintenance tasks (`internal/maintenance/`), starts `Scheduler`, registers Gin routes, and exits gracefully on SIGINT/SIGTERM (3-second shutdown window).

Go module name: `hijarr`. Default port: `8001`.

Shared dependency: `github.com/DeliYuxiang/SRNApiClient` (local `replace ../srnrelay`) — SRN protocol core (Event types, signing, network queries).

### Route Map (`cmd/hijarr/main.go`)

| Path prefix | Handler | Purpose |
|:---|:---|:---|
| `/prowlarr/*`, `/prowlarr` | `proxy.TorznabProxy` | Prowlarr/Torznab proxy — TMDB title translation + tiered fission search |
| `/sonarr/*`, `/sonarr` | `proxy.TVDBMitmProxy` | TVDB/Skyhook MITM — language hijack + Chinese title injection |
| `/api/frontend/*` | `web.RegisterRoutes` | Admin REST API |
| `/srn/api/*` | `web.RegisterRoutes` | SRN event query/management |
| `/assets/*` | embedded static | Vue 3 frontend assets |
| `/, /web, /config, ...` | SPA fallback | Returns embedded `index.html` |

### Key Packages

- **`internal/proxy/tvdb.go`** — `TVDBMitmProxy`: forwards to `skyhook.sonarr.tv`/`api.thetvdb.com`/`api4.thetvdb.com`, hijacks language segments in URLs (e.g. `eng` → `zho`), patches `title`/`name`/episode titles in-place using `gjson`/`sjson`. Handles both Skyhook v1 (`v1/tvdb/shows/`) and TVDB v4 (`v4/series/`, `v4/search`) formats. Paths starting with `/api` are forwarded to `TorznabProxy`.

- **`internal/proxy/torznab.go`** — `TorznabProxy` + `ExecuteProwlarrFissionSearch`: resolves TVDB/IMDB IDs or English titles to TMDB Chinese titles, appends `SxxEyy` suffix, performs tiered fission search (episode → season → series), stops when ≥ 10 results found.

- **`internal/proxy/util.go`** — HTTP helpers: `proxyReq()`, `forwardHeaders()`, `copyResponseHeaders()`.

- **`internal/srn/srn.go`** — SRN facade: `SetNodeKey`, `QueryNetworkForLangs`, `DownloadFromRelays`, `PublishToNetwork`, `RetractEvent`, `ReplaceEvent`, `QueryRelayIdentity`. Type aliases `Event`/`ErrPermanentUpload`/`RelayIdentity` delegate to the shared `srnrelay` module.

- **`internal/srn/provider.go`** — `Provider`: three-priority subtitle query — (1) local `srn_queue` SQLite, (2) `BACKEND_SRN_URL` (local srnfeeder), (3) cloud relay network. Cache key format: `"title|T<tmdbID>|S<n>|E<n>"`.

- **`internal/srn/store.go`** — SQLite `srn_queue` table (with generated virtual columns `tmdb_id`/`title`/`lang`/`season`/`ep`). Core methods: `Enqueue`, `GetTasks`, `MarkFailed` (exponential back-off via `next_retry_at`), `Remove`, `Query`/`QueryEvents`/`GetContent`, `ScanByPubKey` (migration use only), `BacklogStatus`.

- **`internal/scheduler/sonarr_sync.go`** — `SonarrSyncJob`: polls Sonarr for episodes missing subtitles → 3-tier SRN query → atomic write (`O_CREATE|O_EXCL`) subtitle file alongside video → `RescanSeries`. Concurrency limit: 3. Implements `EpisodeSearcher` interface (`SearchEpisode`, `SetSubtitleSelection`, `QuerySubtitles`) for Web UI.

- **`internal/scheduler/scheduler.go`** — `Job` interface + `Triggerable` interface + `Scheduler` (ticker-driven, supports manual trigger via channel, `PauseWhen` predicate for back-pressure).

- **`internal/sonarr/client.go`** — Sonarr v3 API client (`GetAllSeries`/`GetEpisodes`/`GetEpisodeFiles`/`RescanSeries`). `SubtitlePath()` generates Sonarr-compatible subtitle paths (`<base>.<tag>.<ext>`, `zh-hant` → `zh-TW`, others → `zh`).

- **`internal/tmdb/client.go`** — TMDB API v3 (L1 `sync.Map` + L2 SQLite dual-layer cache). Key functions: `FetchSeriesInfo` (by TVDB/IMDB ID), `FetchSeriesInfoByID`, `FetchSeriesInfoByQuery`, `FetchEpisodeTitles`, `FetchSeasonCount`, `FetchSeriesAliases`, `FetchSeriesSearchResults`.

- **`internal/config/config.go`** — All environment variables; `SubtitleLanguage` enum (`zh`/`zh-hans`/`zh-hant`/`zh-bilingual`/`en`); `ManualSeasonOverrides` map for quirky multi-season shows (物语系列, 鬼灭之刃, etc.).

- **`internal/cache/cache.go`** — Generic TMDB API response cache (`cache.Get[T]`/`cache.Set[T]`, L1 `sync.Map` + L2 SQLite `tmdb_cache`).

- **`internal/cache/metadata_cache.go`** — Title recognition result cache (`Metadata`: RawTitle/TMDBID/Title/Season/Episode/Aliases), L1 `sync.Map` + L2 SQLite `metadata_cache`.

- **`internal/state/store.go`** — Core state store, 6 tables: `seen_files`, `failed_files`, `global_state` (KV, holds `srn_priv_key`), `subtitle_selections`, `subtitle_blacklist`, `subtitle_pins`.

- **`internal/maintenance/`** — Maintenance task framework: `TaskStore` (tracks applied state), `TaskRunner` (`RunOneShotMigrations` + `RunCommunityTasks`), `Registry`. One-shot protocol tasks auto-restart process via `syscall.Exec` after successful run.

- **`internal/migrations/migrations.go`** — Registers all maintenance tasks via `Wire(privKeyHex)`. Current: `srn-resign-v2` (upgrades V1 short event IDs to V2 full SHA256).

- **`internal/web/`** — Web Admin API and embedded Vue SPA (`//go:embed frontend_dist`). `api.go`: route registration + handlers. `db_api.go`: DB admin CRUD. `media_library.go`: media library + subtitle apply.

- **`internal/metrics/metrics.go`** — Atomic counters `SRNQueryTotal`/`SRNQueryHit`; `Report()` emits delta hit-rate every 30 min; `CurrentJSON()` for `/api/frontend/stats`.

- **`internal/util/subtitle.go`** — Shared utilities: `DetectSubtitleLang` (zh-hans/zh-hant/zh-bilingual/zh), `ClassifySubtitleFile`, `StripASSAttachments`, `CalculateMD5`/`CalculateFileMD5`, `IsSubtitleFile`, `DeduplicateStrings`.

- **`internal/db/db.go`** — SQLite connection factory (`modernc.org/sqlite`, WAL mode, `foreign_keys=on`).

- **`internal/logger/logger.go`** — Level-based structured logger. Config via `LOG_LEVEL=global[,module=level,...]`. Use `logger.For("module")` to get a `ModuleLogger`.

## Technology Stack

- **Language:** Go 1.25 (`go.mod`)
- **Framework:** [Gin](https://gin-gonic.com/) v1.12
- **SQLite:** `modernc.org/sqlite` (pure Go, CGO-free)
- **JSON manipulation:** `tidwall/gjson` + `tidwall/sjson`
- **XML parsing:** `beevik/etree` (Torznab/RSS)
- **SRN protocol:** `github.com/DeliYuxiang/SRNApiClient` (local module)
- **Containerisation:** Docker multi-stage build (node:22-alpine → golang:1.25-alpine → alpine:latest)

## Building and Running

### Local Development

```bash
# CGO_ENABLED=0 is mandatory — uses modernc.org/sqlite (pure Go)
CGO_ENABLED=0 go build -o hijarr ./cmd/hijarr
./hijarr
```

### Tests

```bash
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go test ./internal/proxy/... -v
CGO_ENABLED=0 go test ./internal/srn/... -v
```

### Regenerate code symbol index (after code changes)

```bash
CGO_ENABLED=0 go run ./tools/coderef > docs/CODEREF.md
```

### Docker

```bash
docker compose up --build
```

> Note: `docker-compose.yml` is a legacy artifact referencing `Dockerfile.golang` and service name `lagarr`. Use the root `Dockerfile` for actual deployments.

## Configuration (Environment Variables)

| Variable | Default | Description |
|:---|:---|:---|
| `PORT` | `8001` | Listen port |
| `TMDB_API_KEY` | (required) | TMDB v3 API Key |
| `TARGET_LANGUAGE` | `zh-CN` | Language for TMDB queries |
| `TVDB_LANGUAGE` | `zho` | Language code injected into TVDB/Skyhook URLs |
| `PROWLARR_TARGET_URL` | `http://prowlarr:9696/2/api` | Prowlarr API endpoint |
| `PROWLARR_API_KEY` | (empty) | Prowlarr API Key |
| `SUBTITLE_SEARCH_TIMEOUT` | `3s` | SRN query timeout (Go duration) |
| `CACHE_DB_PATH` | `/data/hijarr.db` | Primary SQLite path |
| `SRN_DB_PATH` | `=CACHE_DB_PATH` | SRN queue DB path (defaults to primary) |
| `STATE_DB_PATH` | `=CACHE_DB_PATH` | State DB path (defaults to primary) |
| `SONARR_URL` | (empty) | Sonarr address — empty disables SonarrSyncJob |
| `SONARR_API_KEY` | (empty) | Sonarr API Key |
| `SONARR_SYNC_INTERVAL` | `5m` | Sonarr sync interval (Go duration) |
| `SONARR_PATH_PREFIX` | (empty) | Sonarr container path prefix (e.g. `/media`) |
| `LOCAL_PATH_PREFIX` | (empty) | Local mount point prefix (e.g. `/mnt/media`) |
| `BACKEND_SRN_URL` | (empty) | Local srnfeeder URL — Priority 2 SRN query |
| `SRN_RELAY_URLS` | (empty) | Cloud SRN relay URLs (comma-separated) — Priority 3 |
| `SRN_PREFERRED_LANGUAGES` | `zh` | Languages to query from relay (comma-separated). Values: `zh`, `zh-hans`, `zh-hant`, `zh-bilingual`, `en` |
| `SRN_PRIV_KEY` | (empty) | Node Ed25519 private key (128 hex chars). Empty = auto-generate (identity rotates on DB loss — set in production) |
| `SRN_NODE_ALIAS` | (empty) | Human-readable node alias (falls back to pubkey hex) |
| `LOG_LEVEL` | `info` | Log level. Format: `global[,module=level,...]` |

## Development Conventions

- **CGO_ENABLED=0 always** — pure Go SQLite via `modernc.org/sqlite`
- **Do not register static Gin routes before `/*path` wildcard** — causes panic; SPA catch-all must be last
- **Path prefix convention**: `/prowlarr/*` (Torznab), `/sonarr/*` (TVDB MITM), `/api/frontend/*` (Admin API), `/srn/api/*` (SRN REST)
- **`SRNApiClient` is a local replace**: SRN protocol changes must be made in `../srnrelay`
- **Core logic location**: `internal/proxy/` (proxying), `internal/srn/` (SRN client), `internal/scheduler/` (background jobs)
- **Manual season overrides**: add to `ManualSeasonOverrides` map in `internal/config/config.go`
- **Log style**: `log.Info("🎯 [ModuleName] ...")` using `ModuleLogger` from `logger.For()`

<!-- doc-sha: c224d156b9ea049f4ba59dc27046a9ef808f1234 -->
