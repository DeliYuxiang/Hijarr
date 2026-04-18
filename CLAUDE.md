# CLAUDE.md

本文件为 Claude Code（claude.ai/code）提供项目操作指南。


## 语言规范

| 场景 | 语言 |
|------|------|
| 对话、解释、计划 | 中文 |
| Commit Message | 英文 (`feat:`, `fix:`, `docs:` 等) |
| 代码注释、README | 英文(除非有特殊说明) |

---

## 📚 LLM Agent 必读清单

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

Go 模块名为 `hijarr`。入口：`cmd/hijarr/main.go`，监听 `:8001`。

---

## 命令

### 构建与运行

```bash
# 重要：本机使用 Intel ICC 编译器，必须 CGO_ENABLED=0
CGO_ENABLED=0 go build -o hijarr ./cmd/hijarr
./hijarr
```

### 测试

```bash
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go test ./internal/srn/... -v
CGO_ENABLED=0 go test ./internal/proxy/... -v
```

### Docker

```bash
docker compose up --build
```

---

## 架构

### 请求路由（`cmd/hijarr/main.go`）

Gin 路由，固定前缀分发：

| 路径前缀 | 处理器 |
|:---|:---|
| `/prowlarr/*` | `TorznabProxy` — Prowlarr/Torznab 代理，TMDB 翻译搜索词 |
| `/sonarr/*` | `TVDBMitmProxy` — TVDB/Skyhook 元数据 MITM，注入中文标题 |
| `/api/frontend/*` | Web Admin API — 配置/状态/媒体库/偏好/DB 管理 |
| `/srn/api/*` | SRN REST API — 事件查询 |
| `/*` (SPA) | Vue 3 前端静态文件 |

### 关键包说明

- **`internal/proxy/tvdb.go`** — 转发到 `skyhook.sonarr.tv`/`api.thetvdb.com`，改写语言段，用 `gjson`/`sjson` 原地 patch `title`/`name`/集标题。

- **`internal/proxy/torznab.go`** — TVDB/IMDB ID + 英文标题 → 中文，追加 `SxxEyy` 后缀，转发 Prowlarr。

- **`internal/proxy/util.go`** — HTTP 工具函数：`proxyReq()`、`forwardHeaders()`、`copyResponseHeaders()`。

- **`internal/srn/`** — SRN 客户端：
  - `store.go` — SQLite `srn_queue` 表，`Enqueue`/`Query`/`GetContent`
  - `provider.go` — `Provider`：本地 SQLite → Backend-SRN → Cloud Relay 三优先级查询
  - `relay.go` — Cloud relay 网络查询（PoW + Ed25519 鉴权）
  - `event.go` — `Event` 结构体，`EventID`（SHA256）

- **`internal/scheduler/sonarr_sync.go`** — `SonarrSyncJob`：轮询 Sonarr 缺字幕集 → SRN 查询 → 写字幕文件到视频同目录 → `RescanSeries`；取代 Bazarr。

- **`internal/scheduler/scheduler.go`** — `Job` 接口 + `Triggerable` 接口 + `Scheduler`（ticker 驱动，支持手动触发）。

- **`internal/sonarr/client.go`** — Sonarr v3 API 客户端（GetAllSeries/GetEpisodes/GetEpisodeFiles/RescanSeries）。

- **`internal/tmdb/client.go`** — TMDB API v3 + 内存缓存。核心：`FetchSeriesInfo`、`FetchSeriesInfoByQuery`、`FetchEpisodeTitle`、`FetchSeriesAliases`。

- **`internal/config/config.go`** — 全部环境变量。`ManualSeasonOverrides` 番剧季数映射表。

- **`internal/cache/metadata_cache.go`** — TMDB 翻译缓存（L1 sync.Map + L2 SQLite）。

- **`internal/state/store.go`** — 核心状态存储：`seen_files` / `failed_files` / `global_state` / `subtitle_selections` / `subtitle_blacklist` / `subtitle_pins`。

- **`internal/migrations/`** — 一次性维护迁移框架，见 `docs/migration.md`。

- **`internal/web/`** — Web Admin API：
  - `api.go` — `/api/frontend/*` 路由（config/status/jobs/stats/media-library/preferences/srn/tmdb）
  - `db_api.go` — `/api/frontend/db/*` 路由（srn-events/seen-files/failed-files/metadata-cache CRUD）

- **`internal/metrics/metrics.go`** — SRN 查询命中率计数器（`SRNQueryTotal` / `SRNQueryHit`）。

- **`internal/util/subtitle.go`** — 共享工具：`SubtitleExts` map、`IsSubtitleFile()`、`DeduplicateStrings()`。

---

## 环境变量

| 变量 | 默认值 | 说明 |
|:---|:---|:---|
| `PORT` | `8001` | 服务监听端口 |
| `TMDB_API_KEY` | (空) | **必须** — TMDB v3 API Key |
| `TARGET_LANGUAGE` | `zh-CN` | TMDB 查询语言 |
| `TVDB_LANGUAGE` | `zho` | 注入 TVDB URL 的语言码 |
| `PROWLARR_TARGET_URL` | `http://prowlarr:9696/2/api` | Prowlarr API 地址 |
| `PROWLARR_API_KEY` | (空) | Prowlarr API Key |
| `SUBTITLE_SEARCH_TIMEOUT` | `3s` | SRN 查询超时（Go duration 格式） |
| `CACHE_DB_PATH` | `/data/hijarr.db` | SQLite 主数据库路径 |
| `SRN_DB_PATH` | `=CACHE_DB_PATH` | SRN 队列数据库路径（默认同主库） |
| `STATE_DB_PATH` | `=CACHE_DB_PATH` | 状态数据库路径（默认同主库） |
| `SONARR_URL` | (空) | Sonarr 地址，如 `http://sonarr:8989`（空=禁用 SonarrSyncJob） |
| `SONARR_API_KEY` | (空) | Sonarr API Key |
| `SONARR_SYNC_INTERVAL` | `5m` | Sonarr 同步间隔（Go duration 格式） |
| `SONARR_PATH_PREFIX` | (空) | Sonarr 容器内路径前缀（如 `/media`），用于路径转换 |
| `LOCAL_PATH_PREFIX` | (空) | 本地挂载点前缀（如 `/mnt/media`），替换 `SONARR_PATH_PREFIX` |
| `BACKEND_SRN_URL` | (空) | 本地 srnfeeder 地址（如 `http://srnfeeder:8002`），Priority 2 查询 |
| `SRN_RELAY_URLS` | (空) | 云端 SRN Relay URL，逗号分隔，Priority 3 查询 |
| `SRN_PREFERRED_LANGUAGES` | `zh` | 向 relay 查询的语言列表（逗号分隔）。可选值: `zh`, `zh-hans`, `zh-hant`, `zh-bilingual`, `en` |
| `SRN_PRIV_KEY` | (空) | 节点 Ed25519 私钥（128 hex chars）。空=自动生成（每次重建身份会轮换） |
| `SRN_NODE_ALIAS` | (空) | 节点在 SRN 网络中的可读代号（空=回退到公钥 hex） |
| `LOG_LEVEL` | `info` | 日志级别，格式：`全局级别[,模块=级别,...]` |

---

## LLM Agent 行为规范

### 修改代码前

- **先构建，确认干净**：`CGO_ENABLED=0 go build ./...`
- **最小化改动**：不在任务范围外重构代码
- **遵循现有风格**：日志格式 `fmt.Printf("🎯 [模块名] ...")`
- **不随意新建文件**：优先修改已有文件
- **每次逻辑改动后测试**：`CGO_ENABLED=0 go test ./...`

### 硬性约束

- **CGO_ENABLED=0** 永远需要 — 本机 Intel ICC 编译器不兼容 CGO
- **modernc.org/sqlite** — 纯 Go SQLite，无需 CGO
- **不在 `/*path` 前注册静态 Gin 路由** — 会 panic；路由必须是固定前缀，SPA catch-all 在末尾
- **路径前缀约定**：`/prowlarr/*`（Torznab）、`/sonarr/*`（TVDB MITM）、`/api/frontend/*`（Admin API）、`/srn/api/*`（SRN REST）

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

---

## 遗留代码

`src/proxy.py` 和 `Dockerfile.fastapi` 是原 Python/FastAPI 实现，已被 Go 重写取代。不属于当前构建，勿修改。
