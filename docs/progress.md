# Progress.md — Hijarr Project State

Last updated: 2026-04-18

---

## Current Phase: Smart Client SRN

Hijarr 已完成从单体"智能消费者 + 主动生产者"架构到轻量级 **Smart Client** 的重构。
生产侧功能（字幕摄取、RSS/磁盘扫描、qBit、LLM、Fingerprint）计划在独立项目 `srnfeeder` 中实现。

---

## Hijarr 保留功能（Smart Client）

### Core Proxy
- [x] `TVDBMitmProxy` — TVDB/Skyhook 元数据 MITM，语言重写，gjson/sjson patch
- [x] `TorznabProxy` — TVDB/IMDB ID → 中文标题，SxxEyy 后缀，磁力链接重写

### SRN 查询（只读）
- [x] `Provider` — 本地 SQLite 队列 + Backend-SRN + Cloud Relay 三优先级
- [x] SRN 队列（`srn_queue` 表）— 待上传事件本地暂存
- [x] SRN 迁移任务（`srn_resign_v2`）— 事件 ID V1→V2 协议升级

### Sonarr 直连集成（取代 Bazarr）
- [x] `SonarrSyncJob` — 定期同步缺字幕集 → SRN 查询 → 写字幕文件 → `RescanSeries`
- [x] Sonarr v3 API 客户端（GetAllSeries/GetEpisodes/GetEpisodeFiles/RescanSeries）

### 状态持久化
- [x] `subtitle_selections` — 用户字幕选择记录（防回滚）
- [x] `subtitle_blacklist` — 字幕屏蔽列表（event_hash + cache_key + reason）
- [x] `subtitle_pins` — 字幕固定（cache_key → event_id）
- [x] `seen_files` / `failed_files` — 扫盘 mtime 缓存
- [x] `global_state` — Ed25519 私钥存储

### Web UI & API
- [x] Vue 3/Vite 前端
- [x] Media Library — 剧集字幕状态可视化 + 手动选择
- [x] Subtitle Preferences — 黑名单 + Pin 管理（`/api/frontend/preferences/*`）
- [x] DB Manager — srn-events / seen-files / failed-files / metadata-cache 管理
- [x] Config — SRN upstreams / 节点公钥 / 节点代号展示
- [x] Stats — SRN 查询命中率实时统计
- [x] 维护任务框架（`internal/migrations/`）

---

## Active Risks

### Risk 1: Thundering Herd（首次加载竞态）
- **现象**：Bazarr 同时发出 12 个请求，均未命中 SRN 本地缓存，同时触发远端查询
- **影响**：最多 11 个请求可能返回空，副作用下次搜索全部命中
- **现状**：已不在请求热路径（Bazarr 已移除），`SonarrSyncJob` 串行处理不受影响
- **处置**：可接受，在 srnfeeder 实现时修复

---

## Planned Features (Roadmap)

### 🔴 P0 — srnfeeder 独立服务创建

- [ ] 创建 `srnfeeder` 项目骨架，实现生产侧模块（RSS/磁盘扫描/LLM/fingerprint）
- [ ] srnfeeder 暴露 `GET /v1/events`、`GET /v1/events/:id/content`、`POST /v1/feed`
- [ ] Hijarr client 的 `BACKEND_SRN_URL` 配置对接 srnfeeder

### 🟡 P1 — Hijarr 端完善

- [ ] SonarrSyncJob 读取 `subtitle_blacklist` / `subtitle_pins` 影响字幕选择策略

### 🟢 P2 — Ops

- [ ] Prometheus metrics endpoint (`/metrics`)
- [ ] Graceful shutdown with in-flight request draining

### 🔵 P3 — SRN Phase 2（srnfeeder 项目）

- [ ] Ed25519 公网 Relay 发布
- [ ] 多节点 fan-out

---

## File Map (Key Files — Smart Client)

```
cmd/hijarr/main.go              Entry point, routing, scheduler init, DI wiring
tools/coderef/main.go           Go AST code reference generator → docs/CODEREF.md
internal/
  config/config.go              Env vars (Sonarr/SRN/Prowlarr/TMDB/Port)
  util/subtitle.go              SubtitleExts / IsSubtitleFile / DeduplicateStrings
  proxy/
    tvdb.go                     Metadata MITM (TVDB/Skyhook → TMDB patch)
    torznab.go                  Torrent search proxy (TMDB translate + Prowlarr)
    util.go                     HTTP helpers
  scheduler/
    scheduler.go                Job/Triggerable interfaces + Scheduler
    sonarr_sync.go              SonarrSyncJob: Sonarr API → SRN → subtitle files
  sonarr/
    client.go                   Sonarr v3 API client
  srn/
    event.go                    Event struct + EventID
    store.go                    SQLite queue (Enqueue/Query/GetContent)
    provider.go                 SRN Provider (local→backend→relay)
    relay.go                    Cloud relay query (PoW + Ed25519)
  tmdb/client.go                TMDB API v3 + caching
  cache/
    metadata_cache.go           Title resolution cache
  migrations/
    migrations.go               Wire + GlobalRegistry
    srn_resign_v2.go            SRN event ID V1→V2 migration
  maintenance/                  One-shot task runner framework
  state/
    store.go                    Core state: seen_files/failed_files/global_state/
                                subtitle_selections/subtitle_blacklist/subtitle_pins
  metrics/metrics.go            SRN query hit counters
  web/
    api.go                      /api/frontend/* routes + SPA serving
    db_api.go                   /api/frontend/db/* (srn-events/seen-files/failed-files/metadata-cache)
  db/db.go                      SQLite Open (WAL + busy_timeout)
  logger/                       Per-module log level config
frontend/
  src/views/
    MediaLibrary.vue            Sonarr 剧集字幕管理
    SubtitlePreferences.vue     黑名单 + Pin 管理
    DBManager.vue               DB 数据管理
docs/
  CODEREF.md                    Auto-generated symbol index
  migration.md                  One-shot migration framework guide
  progress.md                   本文件
  llm_note/                     Agent 工作日志
```
