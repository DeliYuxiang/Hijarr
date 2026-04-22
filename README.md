# Hijarr — 中文动漫媒体环境的透明代理与字幕生态引擎

[![GitHub](https://img.shields.io/badge/GitHub-DeliYuxiang%2FHijarr-blue?logo=github)](https://github.com/DeliYuxiang/Hijarr)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPL_v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)

Hijarr 是专为 **Sonarr / Prowlarr** 深度定制的中文化增强工具，使用 **Go** 编写。  
名称来源：**Hi**jacking upstream services for Son**arr** / Baz**arr**。

It 通过拦截元数据请求、翻译搜索词、以及直连 **SRN (Subtitle Relay Network)** 协议，实现了从"元数据中文化"到"字幕自动精准下发"的完整闭环。

---

## 模块架构

Hijarr 目前已精简为单一职责的高效核心：**Smart Client (consumption-side focus)**。

```
┌─────────────────────────────────────────────────────┐
│  hijarr-core（消费与管理核心）                       │
│                                                     │
│  Sonarr ──DNS劫持──► Hijarr ──查询──► SRN Relay     │
│                                                     │
│  · Skyhook / TVDB 元数据 MITM (TMDB 注入)            │
│  · Torznab 代理（英文→中文搜索词 + 分级裂变）        │
│  · SRN 字幕同步（MD5 追踪 + 锁定防回滚）             │
│  · SonarrSyncJob（直接写字幕文件到 Sonarr 媒体目录） │
└─────────────────────────────────────────────────────┘
```

> **注**：原 Module 2 (Scrapers/Feeders) 的复杂爬虫逻辑已从本项目剥离。Hijarr 现在作为一个纯粹的 **Smart Client** 运行，深度依赖 [SRN](https://github.com/DeliYuxiang/srn) 协议获取高质量字幕。

---

## 生态系统

| 项目 | 角色 |
| :--- | :--- |
| [**Hijarr**](https://github.com/DeliYuxiang/Hijarr)（本项目） | Smart Client — DNS 劫持、字幕同步、媒体库 MD5 管理 |
| [**SRN**](https://github.com/DeliYuxiang/SubtitleRelayNetwork) | 去中心化字幕中继协议，Hijarr 的首选查询源与内容提供商 |
| [**srnrelay**](../srnrelay) | Go 共享模块：SRN 协议核心（Event 类型、签名、网络查询），Hijarr 通过本地 `replace` 依赖 |
| [**Sonarr**](https://github.com/Sonarr/Sonarr) | 剧集管理，Hijarr 拦截其元数据请求并直连同步字幕 |
| [**Caddy**](https://github.com/caddyserver/caddy) | 反向代理，建议作为 Hijarr 的 TLS 终止层与访问控制层 |

---

## 核心功能 (Technical Highlights)

### 1. 透明元数据劫持 (Metadata Proxy)
通过 DNS 劫持拦截 Sonarr 的元数据请求，利用 `gjson/sjson` 实时改写：
- **Skyhook / TVDB 劫持**：拦截 `skyhook.sonarr.tv`、`api.thetvdb.com`、`api4.thetvdb.com`，通过 TMDB 实时将剧名、季名、集标题翻译为中文。同时支持 Skyhook v1 API 和 TVDB v4 API 两套格式。
- **语言段自动劫持**：自动检测并将 URL 中的语言请求（如 `eng`）重写为目标语言（`zho`）。

### 2. Torznab 代理与裂变搜索 (Prowlarr Fission)
- **语义搜索转换**：将 Sonarr 的 TVDB/IMDB ID 或英文关键词自动转为 TMDB 中文标题，解决中文动漫索引器搜不到英文名的问题。
- **分级裂变 (Tiered Search)**：自动执行 `集 -> 季 -> 系列` 的三级阶梯式搜索，≥10 条结果即停，最大化提升下载命中率。
- **季数覆盖 (Manual Mapping)**：内置 `ManualSeasonOverrides`（如物语系列、鬼灭之刃等），解决国内外季数定义偏移问题。

### 3. SRN 字幕同步与 MD5 管理 (Subtitle Engine)
- **取代 Bazarr**：`SonarrSyncJob` 定时扫描 Sonarr 媒体库（默认每 5 分钟），为缺失字幕的视频直接从 SRN 拉取并写入本地目录，触发 Sonarr `RescanSeries`。
- **三级优先级查询**：(1) 本地 SQLite 队列 → (2) 本地 srnfeeder 实例 (`BACKEND_SRN_URL`) → (3) 云端 Relay 网络 (`SRN_RELAY_URLS`)。
- **MD5 追踪与锁定 (Pinning)**：
    - 记录视频关联字幕的 `sub_md5` 与归档包 `archive_md5`。
    - **手动选择记忆**：用户通过 Web UI 手动"应用"特定字幕版本后，系统会建立锁定关系，防止后续自动同步将其回退到低质量版本。
- **黑名单与固定**：可在 Web UI 中将质量差的字幕加入黑名单，或将特定版本固定（pin）为首选。

### 4. Web 管理界面
内嵌 Vue 3 单页应用，提供：
- **媒体库**：浏览 Sonarr 全部剧集，查看各集字幕状态，手动触发字幕搜索或应用。
- **SRN 管理**：浏览/删除本地 SRN 队列事件。
- **数据库管理**：查看/编辑元数据缓存、已扫描文件、处理失败文件。
- **偏好设置**：管理字幕黑名单与固定（pin）。
- **配置/状态/统计**：运行时配置概览、SRN 命中率统计。

---

## 为什么移除 Bazarr

Bazarr 被彻底踢出支持列表，原因如下：

**1. 上游字幕提供商无有效匹配**  
字幕提供商不提供字幕到媒体元数据的最大努力匹配。字幕组以原始文件名直接上传再分发，字幕文件名与 Sonarr 媒体元数据之间的映射关系完全由 Hijarr 负责建立。Bazarr 作为中间层在此失去意义。

**2. Bazarr 对文件名极度挑食**  
即使 Hijarr 已完成匹配、重命名、本地 CDN 托管，Bazarr 仍会以自身的文件名校验逻辑大概率拒绝字幕。

**3. 上游字幕网站已基本死亡**  
原 Bazarr 字幕源（assrt.net 等）可用性极低。Hijarr 现在的主要字幕来源是通过 SRN 协议分发的高质量字幕包。

**现状**：`SonarrSyncJob` 直接对接 Sonarr API，完全取代 Bazarr 的字幕管理职责。

---

## 架构概览

```
Sonarr / Prowlarr
  │
  ├─ Metadata Proxy (skyhook/TVDB) ──► TVDBMitmProxy → TMDB API (gjson/sjson patch)
  │
  ├─ Torznab Proxy (/prowlarr) ─────► TorznabProxy → TMDB translate → Prowlarr → Fission Search
  │
  └─ Web Admin / Media Library ─────► WebAPI → Sonarr Sync Control + SRN Manual Selection

后台调度器 (Scheduler)
  └─ SonarrSyncJob  → Sonarr API → SRN Provider (Local→Backend→Cloud) → Write Subtitle to Video Dir

社区维护系统 (Maintenance)
  └─ srn-resign-v2  → 一次性迁移：V1 短 ID → V2 完整 SHA256，执行后自动重启
```

详细文件地图见 [docs/CODEREF.md](./docs/CODEREF.md)（自动生成）。

---

## 社区维护任务（Community Maintenance Tasks）

Hijarr 内置一套灵活的**社区维护任务**系统。除了处理协议变更的一次性修复（如签名算法升级）外，它还允许 client 领取并处理 SRN 全网的维护工作。

**运行语义**：
- **一次性任务 (protocol)**：在 boot 阶段同步检查。若有未执行的任务（如 `srn-resign-v2`），阻塞执行并完成后自动 `syscall.Exec` **原地重启**进程。
- **社区选配任务 (cleanup/stats)**：允许 client 根据自身配置，"领取"特定类别的全网维护工作。

**任务类别**：
- `protocol`：协议级强制升级（一次性运行 + 重启）。
- `cleanup`：SRN 网络清理（如撤销过期或错误的字幕）。
- `stats`：统计与元数据整合任务。

**当前已注册任务**：
- `srn-resign-v2`：将本节点本地队列中的 V1 短 ID（32 hex）事件升级为 V2 完整 SHA256 ID（64 hex），同时向 relay 发送 `Kind 1003` 替换通知。

---

## 快速开始

### Docker Compose 部署 (推荐)

```yaml
services:
  hijarr:
    image: yuxiang/hijarr:latest
    container_name: hijarr
    ports:
      - "8001:8001"
    environment:
      - PORT=8001
      - TMDB_API_KEY=your_tmdb_api_key

      - SONARR_URL=http://sonarr:8989
      - SONARR_API_KEY=your_sonarr_api_key
      - PROWLARR_TARGET_URL=http://prowlarr:9696/2/api
      - PROWLARR_API_KEY=your_prowlarr_api_key
      - SRN_RELAY_URLS=https://srn.example.com
      # 节点身份（生产必须固化，否则重建容器后身份轮换）
      - SRN_PRIV_KEY=your_128_hex_char_ed25519_private_key
      # 路径映射：Sonarr 容器内路径前缀 vs Hijarr 挂载点
      - SONARR_PATH_PREFIX=/media
      - LOCAL_PATH_PREFIX=/mnt/media
      - TARGET_LANGUAGE=zh-CN
      - TVDB_LANGUAGE=zho
    volumes:
      - ./data:/data
      - /mnt/media:/mnt/media # 必须挂载与 Sonarr 对应的视频目录
    dns:
      - 8.8.8.8  # 必须使用公网 DNS，避免 DNS 劫持循环
```

### DNS 劫持配置
为了使元数据翻译生效，需将以下域名劫持到 Hijarr（经由 Caddy TLS 终止）：
- `skyhook.sonarr.tv`
- `api.thetvdb.com`
- `api4.thetvdb.com`

详细的 DNS 劫持与 Caddy TLS 终止配置，见 [DNS_HIJACKING_GUIDE.md](./DNS_HIJACKING_GUIDE.md)。

---

## 环境参数

| 变量 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `PORT` | `8001` | 服务监听端口 |
| `TMDB_API_KEY` | (必填) | TMDB API v3 Key |
| `TARGET_LANGUAGE` | `zh-CN` | TMDB 查询语言（标题翻译） |
| `TVDB_LANGUAGE` | `zho` | 注入 TVDB/Skyhook URL 的语言码 |
| `SONARR_URL` | (空) | Sonarr 地址（启用同步任务必须） |
| `SONARR_API_KEY` | (空) | Sonarr API Key |
| `SONARR_SYNC_INTERVAL` | `5m` | 媒体库字幕同步频率 |
| `SONARR_PATH_PREFIX` | (空) | Sonarr 容器内路径前缀 |
| `LOCAL_PATH_PREFIX` | (空) | 本地挂载点前缀（替换 `SONARR_PATH_PREFIX`） |
| `PROWLARR_TARGET_URL` | `http://prowlarr:9696/2/api` | Prowlarr API 地址 |
| `PROWLARR_API_KEY` | (空) | Prowlarr API Key |
| `SRN_RELAY_URLS` | (空) | SRN 云端中继节点，逗号分隔，Priority 3 |
| `BACKEND_SRN_URL` | (空) | 本地 srnfeeder 地址，Priority 2 |
| `SRN_PREFERRED_LANGUAGES` | `zh` | relay 查询语言（逗号分隔）。可选: `zh`, `zh-hans`, `zh-hant`, `zh-bilingual`, `en` |
| `SRN_PRIV_KEY` | (空) | 节点 Ed25519 私钥（128 hex chars）。**生产必须固化** |
| `SRN_NODE_ALIAS` | (空) | 节点可读代号（空=回退到公钥 hex） |
| `CACHE_DB_PATH` | `/data/hijarr.db` | SQLite 数据库路径 |
| `LOG_LEVEL` | `info` | 日志级别 (debug/info/warn/error) |

### LOG_LEVEL 详细说明

格式：`全局级别[,模块名=级别,...]`，级别可选 `debug` / `info` / `warn` / `error`。

```bash
LOG_LEVEL=debug                        # 全部模块开 debug
LOG_LEVEL=info,proxy=debug             # 只看 proxy 模块的 debug 日志
LOG_LEVEL=info,sonarr=debug,srn-store=debug
```

可用模块名：

| 模块名 | 覆盖范围 | 典型 debug 内容 |
| :--- | :--- | :--- |
| `proxy` | `internal/proxy/` | Torznab 翻译查询、TVDB/Skyhook 元数据改写 |
| `srn-store` | `internal/srn/provider.go` | SRN 本地 SQLite 事件存储、Query 命中 |
| `cache` | `internal/cache/metadata_cache.go` | TMDB 翻译缓存读写 |
| `sonarr` | `internal/scheduler/sonarr_sync.go` | 缺字幕集扫描、SRN 命中、字幕写盘 |
| `scheduler` | `internal/scheduler/scheduler.go` | Job 注册、ticker 触发、手动触发 |

---

## 构建与开发

```bash
# 全量构建（必须 CGO_ENABLED=0，使用纯 Go SQLite）
CGO_ENABLED=0 go build -o hijarr ./cmd/hijarr

# 运行所有测试
CGO_ENABLED=0 go test ./...

# 更新代码符号速查表（代码改动后必须执行）
CGO_ENABLED=0 go run ./tools/coderef > docs/CODEREF.md

# Docker
docker compose up --build
```

---

## For LLM Agents

在修改任何代码之前，必须按顺序阅读：

1. [CLAUDE.md](CLAUDE.md) — 构建命令、架构地图、Agent 行为规范
2. [docs/CODEREF.md](docs/CODEREF.md) — **自动生成的符号速查表**（首选参考，避免重复造轮子）
3. [docs/progress.md](docs/progress.md) — 当前项目状态、已知风险、路线图
4. [docs/llm_note/](docs/llm_note/) — 历次 Agent 工作日志（按时间顺序）

---

## 演进路线

| 阶段 | 状态 | 目标 |
| :--- | :--- | :--- |
| Phase 1：本地寄生模式 | ✅ 已完成 | 本地 SQLite 节点，自动拆解季包，积累字幕数据 |
| Phase 1.5：SRN 剥离 | ✅ 已完成 | SRN 协议层独立为共享模块（srnrelay），Cloudless v2.x |
| Phase 2：模块拆分 | 规划中 | Proxy（消费侧）与 Feeder（生产侧）分拆为独立可部署单元 |
| Phase 2.5：上传器独立 | 规划中 | Feeder 进一步拆分：纯上传 SDK/CLI + 爬取清洗核心 |
| Phase 3：自治网络 | 规划中（SRN 项目） | NIPs 规范，PoW/微支付防滥用，AI 自治节点 |

---

## 许可证

本项目以 [GNU AGPL v3](LICENSE) 开源。个人使用及开源项目免费；若将其作为网络服务商用且不愿开放修改，需获取商业授权。

**模块开源计划：**

| 模块 | 开源计划 |
| :--- | :--- |
| [`hijarr-proxy`](https://github.com/DeliYuxiang/Hijarr)（消费侧：DNS 劫持、字幕聚合、Sonarr 同步） | AGPL v3 开源 |
| `hijarr-uploader`（SRN 上传 SDK/CLI） | AGPL v3 开源 |
| `hijarr-scraper`（爬取 + 清洗核心） | 不开源 |

> 贡献者通过提交 PR 即视为同意 [CONTRIBUTING.md](./CONTRIBUTING.md) 中的贡献者协议，不可撤销地将该贡献的全部著作权转让给项目维护者。

<!-- doc-sha: c224d156b9ea049f4ba59dc27046a9ef808f1234 -->
