# CLAUDE.md

本文件为 Claude Code（claude.ai/code）提供项目操作指南。


## 语言规范

| 场景 | 语言 |
|------|------|
| 对话、解释、计划 | 中文 |
| Commit Message | 英文 (`feat:`, `fix:`, `docs:` 等) |
| 代码注释、README | 英文(除非有特殊说明) |

---

## LLM Agent 必读清单

在对本项目做任何修改之前，必须**按顺序**阅读以下文档：

1. **本文件（CLAUDE.md）** — 构建命令、架构地图、环境变量、Agent 行为规范
2. **[docs/CODEREF.md](docs/CODEREF.md)** — **自动生成的符号速查表**（Symbol Index/路由/DB Schema/依赖图）— 跨文件开发的首选参考，避免重复造轮子
3. **[README.md](README.md)** — 面向用户的功能概述和快速开始
4. **[docs/progress.md](docs/progress.md)** — 当前项目状态、活跃风险、路线图
5. **[docs/llm_note/](docs/llm_note/)** — 历次 Agent 工作日志（按时间顺序阅读）
6. **[docs/llm_note/NOTE_TEMPLATE.md](docs/llm_note/NOTE_TEMPLATE.md)** — 工作日志模板

阅读完毕后，在做任何修改前**先创建本次会话的工作日志**：
```
docs/llm_note/note_YYYYMMDD_<agent>.md
```

---

## 项目概述

**Hijarr** 是一个为中文媒体环境深度定制的 Go Smart Client 代理，服务于 Sonarr/Prowlarr。
它拦截发往 `skyhook.sonarr.tv`、`api.thetvdb.com` 的请求，
用 TMDB 中文翻译重写元数据，并通过 **SRN（字幕中继网络）** 查询和落盘高质量中文字幕。

Go 模块名为 `hijarr`（`go.mod` 中声明）。入口：`cmd/hijarr/main.go`，默认监听 `:8001`。

**共享依赖**：`github.com/DeliYuxiang/SRNApiClient`（本地 `replace` 指向 `../srnrelay`）— SRN 协议核心（Event 类型、签名、网络查询）。

---

## 命令

### 构建与运行

```bash
# 重要：CGO_ENABLED=0 确保使用纯 Go SQLite（modernc.org/sqlite），避免 CGO 兼容问题
CGO_ENABLED=0 go build -o hijarr ./cmd/hijarr
./hijarr
```

### 测试

```bash
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go test ./internal/srn/... -v
CGO_ENABLED=0 go test ./internal/proxy/... -v
```

### 代码符号速查表（修改代码后必须重新生成）

```bash
CGO_ENABLED=0 go run ./tools/coderef > docs/CODEREF.md
```

### Docker

```bash
docker compose up --build
```

> **注意**：`docker-compose.yml` 中的配置是旧版遗留（引用 `lagarr`/`Dockerfile.golang`），实际部署应直接使用 `Dockerfile`（多阶段构建：node 前端 + Go 后端 + Alpine 运行镜像）。

---

## 架构

### 请求路由（`cmd/hijarr/main.go`）

Gin 路由，固定前缀分发：

| 路径前缀 | 处理器 | 说明 |
|:---|:---|:---|
| `/prowlarr/*` 或 `/prowlarr` | `proxy.TorznabProxy` | Prowlarr/Torznab 代理，TMDB 翻译搜索词 + 分级裂变 |
| `/sonarr/*` 或 `/sonarr` | `proxy.TVDBMitmProxy` | TVDB/Skyhook 元数据 MITM，注入中文标题 |
| `/api/frontend/*` | `web.RegisterRoutes` — Admin API | 配置/状态/媒体库/偏好/DB 管理 |
| `/srn/api/*` | `web.RegisterRoutes` — SRN API | SRN 事件查询/管理 |
| `/assets/*` | 静态文件服务 | 嵌入式 Vue 前端资产 |
| `/, /web, /config, /jobs, /stats, /media, /preferences, /db` | SPA fallback | 返回 Vue SPA 入口 `index.html` |

### 完整 API 端点表

#### `/api/frontend/*` — Admin REST API

| Method | Path | 说明 |
|:---|:---|:---|
| GET | `/api/frontend/config` | 运行时配置概览（URLs、语言、密钥别名等） |
| GET | `/api/frontend/status` | 运行时状态（uptime、版本） |
| GET | `/api/frontend/stats` | SRN 查询命中率统计 |
| GET | `/api/frontend/jobs` | 已注册调度任务列表 |
| GET | `/api/frontend/media-library` | Sonarr 剧集列表 |
| GET | `/api/frontend/media-library/:id` | 单部剧集详情（季/集/字幕状态） |
| POST | `/api/frontend/search-episode` | 手动触发单集字幕搜索 |
| POST | `/api/frontend/apply-subtitle` | 手动应用指定 SRN 字幕到视频目录 |
| GET | `/api/frontend/tmdb/season-count` | 查询 TMDB 剧集季数 |
| GET | `/api/frontend/tmdb/search` | TMDB 剧集搜索（自动补全） |
| GET | `/api/frontend/db/metadata-cache` | 元数据缓存列表（分页） |
| POST | `/api/frontend/db/metadata-cache/delete` | 删除元数据缓存条目 |
| POST | `/api/frontend/db/metadata-cache/upsert` | 手动写入/修改元数据缓存 |
| GET | `/api/frontend/db/srn-events` | SRN 队列事件列表（分页） |
| POST | `/api/frontend/db/srn-events/delete` | 删除 SRN 队列事件 |
| GET | `/api/frontend/db/seen-files` | 已扫描文件记录（分页） |
| POST | `/api/frontend/db/seen-files/delete` | 删除已扫描文件记录 |
| GET | `/api/frontend/db/failed-files` | 处理失败文件记录（分页） |
| POST | `/api/frontend/db/failed-files/delete` | 删除失败文件记录 |
| GET | `/api/frontend/preferences` | 字幕偏好（黑名单 + 固定） |
| POST | `/api/frontend/preferences/blacklist` | 添加字幕黑名单 |
| DELETE | `/api/frontend/preferences/blacklist/:hash` | 移除字幕黑名单 |
| POST | `/api/frontend/preferences/pin` | 固定指定字幕版本 |
| DELETE | `/api/frontend/preferences/pin` | 解除字幕固定 |

#### `/srn/api/*` — SRN API

| Method | Path | 说明 |
|:---|:---|:---|
| GET | `/srn/api/search` | SRN 字幕查询（`tmdb_id`, `s`, `e` 参数） |
| GET | `/srn/api/events` | 本地 SRN 队列事件浏览 |
| DELETE | `/srn/api/events/:id` | 删除本地 SRN 队列事件 |

### 关键包说明

- **`cmd/hijarr/main.go`** — 入口：初始化 logger、配置摘要、SRN 节点身份（Ed25519 私钥优先级：`SRN_PRIV_KEY` 环境变量 > 数据库 > 自动生成）、运行维护任务（`maintenance.Runner`）、启动 `Scheduler`、注册 Gin 路由、优雅关机（3s timeout）。

- **`internal/proxy/tvdb.go`** — `TVDBMitmProxy`：转发到 `skyhook.sonarr.tv`/`api.thetvdb.com`/`api4.thetvdb.com`，自动劫持 URL 中的语言段（如 `eng` → `zho`），用 `gjson`/`sjson` 原地 patch `title`/`name`/集标题。支持 Skyhook v1、TVDB v4 两套 API 格式。路径 `/api` 前缀自动转发给 `TorznabProxy`。

- **`internal/proxy/torznab.go`** — `TorznabProxy` + `ExecuteProwlarrFissionSearch`：TVDB/IMDB ID + 英文标题 → TMDB 中文，追加 `SxxEyy` 后缀，执行三级裂变搜索（集 → 季 → 系列），默认阈值 10 条即停。

- **`internal/proxy/util.go`** — HTTP 工具函数：`proxyReq()`、`forwardHeaders()`、`copyResponseHeaders()`，共享 `log = logger.For("proxy")`。

- **`internal/srn/srn.go`** — SRN 门面：`SetNodeKey`、`QueryNetworkForLangs`、`DownloadFromRelays`、`PublishToNetwork`、`RetractEvent`、`ReplaceEvent`、`QueryRelayIdentity`。类型别名：`Event`、`ErrPermanentUpload`、`RelayIdentity` 直接来自 `srnrelay` 共享模块。

- **`internal/srn/provider.go`** — `Provider`：实现三级优先级查询——(1) 本地 `srn_queue` SQLite、(2) `BackendSRNURL`（本地 srnfeeder 实例）、(3) 云端 Relay 网络。缓存 key 格式：`"title|T<tmdbID>|S<n>|E<n>"`。

- **`internal/srn/store.go`** — SQLite `srn_queue` 表（带虚拟列 `tmdb_id`/`title`/`lang`/`season`/`ep`）。核心方法：`Enqueue`、`GetTasks`、`MarkFailed`（指数退避）、`Remove`、`Query`/`QueryEvents`/`GetContent`、`ScanByPubKey`（迁移专用）、`BacklogStatus`。

- **`internal/scheduler/sonarr_sync.go`** — `SonarrSyncJob`：轮询 Sonarr 缺字幕集 → SRN 三级查询 → 原子写字幕文件（`O_CREATE|O_EXCL`）到视频同目录 → `RescanSeries`；并发度 `sonarrMaxConcurrency=3`；取代 Bazarr。同时实现 `EpisodeSearcher` 接口（`SearchEpisode`、`SetSubtitleSelection`、`QuerySubtitles`）供 Web UI 调用。

- **`internal/scheduler/scheduler.go`** — `Job` 接口 + `Triggerable` 接口 + `Scheduler`（ticker 驱动，支持手动触发；支持 `PauseWhen` 谓词，队列积压时自动暂停定时任务）。

- **`internal/sonarr/client.go`** — Sonarr v3 API 客户端（`GetAllSeries`/`GetEpisodes`/`GetEpisodeFiles`/`RescanSeries`）；`SubtitlePath()` 函数生成 Sonarr 兼容的字幕文件路径（`<base>.<langTag>.<ext>`，`zh-hant` → `zh-TW`，其余 → `zh`）。

- **`internal/tmdb/client.go`** — TMDB API v3（L1 sync.Map + L2 SQLite 双层缓存）。核心：`FetchSeriesInfo`（by TVDB/IMDB ID）、`FetchSeriesInfoByID`（by TMDB ID）、`FetchSeriesInfoByQuery`、`FetchEpisodeTitles`、`FetchSeasonCount`、`FetchSeriesAliases`、`FetchSeriesSearchResults`。

- **`internal/config/config.go`** — 全部环境变量（`getEnv`/`getEnvDuration`/`getEnvInt`/`getEnvFloat`）；`SubtitleLanguage` 类型枚举（`zh`/`zh-hans`/`zh-hant`/`zh-bilingual`/`en`）；`ManualSeasonOverrides` 番剧季数映射表。

- **`internal/cache/cache.go`** — TMDB API 响应泛型缓存（L1 `sync.Map` + L2 SQLite `tmdb_cache` 表），由 `cache.Get[T]`/`cache.Set[T]` 访问。

- **`internal/cache/metadata_cache.go`** — 标题识别结果缓存（`Metadata` 结构体：RawTitle/TMDBID/Title/Season/Episode/Aliases），L1 `sync.Map` + L2 SQLite `metadata_cache` 表。

- **`internal/state/store.go`** — 核心状态存储，管理 6 张表：`seen_files`（扫描记录）、`failed_files`（处理失败记录）、`global_state`（KV，含 `srn_priv_key`）、`subtitle_selections`（用户字幕选择记忆）、`subtitle_blacklist`（字幕黑名单）、`subtitle_pins`（字幕固定）。

- **`internal/maintenance/`** — 维护任务框架：`TaskStore`（记录任务执行状态）、`TaskRunner`（`RunOneShotMigrations` + `RunCommunityTasks`）、`Registry`（任务注册）。一次性 protocol 任务执行成功后自动 `syscall.Exec` 重启。

- **`internal/migrations/`** — 具体迁移任务注册：`migrations.Wire(privKeyHex)` 注册所有已知任务；当前唯一任务 `srn-resign-v2`（将 V1 短 ID 升级为 V2 完整 SHA256）。

- **`internal/web/api.go`** — `/api/frontend/*` 路由注册 + handler（config/status/jobs/stats/media-library/preferences/srn/tmdb）。内嵌 Vue 前端（`//go:embed frontend_dist`）。

- **`internal/web/db_api.go`** — `/api/frontend/db/*` 路由（metadata-cache/srn-events/seen-files/failed-files CRUD）。

- **`internal/web/media_library.go`** — 媒体库 handler（`handleMediaLibrary`/`handleMediaLibrarySeries`/`handleSearchEpisode`/`handleApplySubtitle`）；`EpisodeSearcher` 接口定义。

- **`internal/metrics/metrics.go`** — 原子计数器：`SRNQueryTotal` / `SRNQueryHit`；`Report()` 每 30 分钟输出 delta 命中率；`CurrentJSON()` 供 `/api/frontend/stats` 使用。

- **`internal/util/subtitle.go`** — 共享工具：`SubtitleExts`、`IsSubtitleFile`、`DetectSubtitleLang`（zh-hans/zh-hant/zh-bilingual/zh 四档）、`ClassifySubtitleFile`（Normal/Season0/Extras/Skip）、`StripASSAttachments`、`CalculateMD5`/`CalculateFileMD5`、`DeduplicateStrings`、`ExtractGroup`/`ExtractResolution`。

- **`internal/db/db.go`** — SQLite 连接工厂（`modernc.org/sqlite`，WAL 模式，`foreign_keys=on`），返回 `*sql.DB`。

- **`internal/logger/logger.go`** — 结构化日志（level-based，支持 `LOG_LEVEL=全局级别[,模块=级别,...]`，`For("module")` 返回 `ModuleLogger`）。

---

## 环境变量

| 变量 | 默认值 | 说明 |
|:---|:---|:---|
| `PORT` | `8001` | 服务监听端口 |
| `TMDB_API_KEY` | (空) | **必须** — TMDB v3 API Key |
| `TARGET_LANGUAGE` | `zh-CN` | TMDB 查询语言（影响标题翻译） |
| `TVDB_LANGUAGE` | `zho` | 注入 TVDB/Skyhook URL 的语言码 |
| `PROWLARR_TARGET_URL` | `http://prowlarr:9696/2/api` | Prowlarr API 地址 |
| `PROWLARR_API_KEY` | (空) | Prowlarr API Key |
| `SUBTITLE_SEARCH_TIMEOUT` | `3s` | SRN 查询超时（Go duration 格式） |
| `CACHE_DB_PATH` | `/data/hijarr.db` | SQLite 主数据库路径 |
| `SRN_DB_PATH` | `=CACHE_DB_PATH` | SRN 队列数据库路径（默认同主库） |
| `STATE_DB_PATH` | `=CACHE_DB_PATH` | 状态数据库路径（默认同主库） |
| `LOCAL_DOWNLOAD_PATHS` | (空) | 保留字段（srnfeeder 兼容性），hijarr 本身不使用 |
| `SONARR_URL` | (空) | Sonarr 地址，如 `http://sonarr:8989`（空=禁用 SonarrSyncJob） |
| `SONARR_API_KEY` | (空) | Sonarr API Key |
| `SONARR_SYNC_INTERVAL` | `5m` | Sonarr 同步间隔（Go duration 格式） |
| `SONARR_PATH_PREFIX` | (空) | Sonarr 容器内路径前缀（如 `/media`），用于路径转换 |
| `LOCAL_PATH_PREFIX` | (空) | 本地挂载点前缀（如 `/mnt/media`），替换 `SONARR_PATH_PREFIX` |
| `BACKEND_SRN_URL` | (空) | 本地 srnfeeder 地址（如 `http://srnfeeder:8001`），Priority 2 查询 |
| `SRN_RELAY_URLS` | (空) | 云端 SRN Relay URL，逗号分隔，Priority 3 查询 |
| `SRN_PREFERRED_LANGUAGES` | `zh` | 向 relay 查询的语言列表（逗号分隔，有序）。可选值: `zh`, `zh-hans`, `zh-hant`, `zh-bilingual`, `en` |
| `SRN_PRIV_KEY` | (空) | 节点 Ed25519 私钥（128 hex chars）。空=自动生成（每次重建 DB 身份会轮换，生产必须固化） |
| `SRN_NODE_ALIAS` | (空) | 节点在 SRN 网络中的可读代号（空=回退到公钥 hex） |
| `LOG_LEVEL` | `info` | 日志级别，格式：`全局级别[,模块=级别,...]` |

### LOG_LEVEL 可用模块名

| 模块名 | 对应包 | 典型 debug 内容 |
|:---|:---|:---|
| `proxy` | `internal/proxy/` | Torznab 翻译查询、TVDB/Skyhook 元数据改写 |
| `srn-store` | `internal/srn/provider.go` | SRN 本地 SQLite 事件存储、Query 命中 |
| `cache` | `internal/cache/metadata_cache.go` | TMDB 翻译缓存读写 |
| `sonarr` | `internal/scheduler/sonarr_sync.go` | 缺字幕集扫描、SRN 命中、字幕写盘 |
| `scheduler` | `internal/scheduler/scheduler.go` | Job 注册、ticker 触发、手动触发 |

---

## SQLite 数据库结构

所有三个路径（`CACHE_DB_PATH`/`SRN_DB_PATH`/`STATE_DB_PATH`）默认指向同一文件 `/data/hijarr.db`。

| 表名 | 所在初始化代码 | 用途 |
|:---|:---|:---|
| `tmdb_cache` | `internal/cache/cache.go` | TMDB API 响应缓存（泛型 KV） |
| `metadata_cache` | `internal/cache/metadata_cache.go` | 标题识别结果（RawTitle → TMDB 元数据） |
| `srn_queue` | `internal/srn/store.go` | SRN 本地事件队列 + 内容 blob（含虚拟列） |
| `seen_files` | `internal/state/store.go` | 磁盘扫描 mtime 记录 |
| `failed_files` | `internal/state/store.go` | 处理失败文件记录（TTL 黑名单） |
| `global_state` | `internal/state/store.go` | KV 状态（`srn_priv_key` 等） |
| `subtitle_selections` | `internal/state/store.go` | 用户字幕选择记忆（video_path → MD5） |
| `subtitle_blacklist` | `internal/state/store.go` | 字幕黑名单（event_hash） |
| `subtitle_pins` | `internal/state/store.go` | 字幕固定（cache_key → event_id） |

---

## LLM Agent 行为规范

### 修改代码前

- **先构建，确认干净**：`CGO_ENABLED=0 go build ./...`
- **最小化改动**：不在任务范围外重构代码
- **遵循现有风格**：日志格式 `log.Info("🎯 [模块名] ...")`（`ModuleLogger`）或 `fmt.Printf`（main.go）
- **不随意新建文件**：优先修改已有文件
- **每次逻辑改动后测试**：`CGO_ENABLED=0 go test ./...`

### 硬性约束

- **CGO_ENABLED=0** 永远需要 — 使用 `modernc.org/sqlite`（纯 Go SQLite）
- **不在 `/*path` 前注册静态 Gin 路由** — 会 panic；路由必须是固定前缀，SPA catch-all 在末尾
- **路径前缀约定**：`/prowlarr/*`（Torznab）、`/sonarr/*`（TVDB MITM）、`/api/frontend/*`（Admin API）、`/srn/api/*`（SRN REST）
- **SRNApiClient 是本地 replace**：`go.mod` 中 `replace github.com/DeliYuxiang/SRNApiClient => ../srnrelay`，修改 SRN 协议逻辑需在 `../srnrelay` 中进行

### 活跃风险

见 `docs/progress.md` → **Active Risks** 章节。

### 工作流程

```
1. 按顺序阅读所有必读文档（见本文顶部）
2. 创建工作日志：docs/llm_note/note_YYYYMMDD_<agent>.md
3. 查看 docs/progress.md 了解活跃风险和当前状态
4. CGO_ENABLED=0 go build ./...（确认干净）
5. 做最小化、有针对性的修改
6. CGO_ENABLED=0 go test ./...
7. CGO_ENABLED=0 go run ./tools/coderef > docs/CODEREF.md（代码有改动时必须重新生成）
8. 如有功能状态变化，更新 docs/progress.md
9. 完善工作日志（结果、建议）
```

### 做 / 不做

✅ **要做：**
- 修改代码前先读完所有文档
- 修改前后均运行构建 + 测试
- 每次会话写工作日志
- 完成/发现工作项时更新 `docs/progress.md`
- 只在逻辑不自明处加注释

❌ **不做：**
- 跳过 `CGO_ENABLED=0`
- 在 SPA catch-all 旁边注册额外 Gin 路由（会 panic）
- 添加路线图之外的投机性功能
- 实现 srnfeeder 侧的功能（爬虫/LLM/磁盘扫描/qBit/RSS）到本项目
- 直接修改 `docs/CODEREF.md`（由工具自动生成）

---

## 遗留代码

`src/proxy.py` 和 `Dockerfile.fastapi` 是原 Python/FastAPI 实现，已被 Go 重写取代。不属于当前构建，勿修改。

`docker-compose.yml` 引用了已不存在的 `Dockerfile.golang` 和旧服务名 `lagarr`，仅作历史遗留，实际 Docker 部署使用根目录 `Dockerfile`。

<!-- doc-sha: c224d156b9ea049f4ba59dc27046a9ef808f1234 -->
