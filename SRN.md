# SRN (Subtitle Relay Network) — Hijarr 集成文档

本文档描述 Hijarr 作为 **Smart Client** 与 SRN 网络交互的实现细节。  
关于 SRN 协议规范本身，请参阅 [../srnrelay](../srnrelay) 共享模块文档。

---

## 架构概述

Hijarr 通过 `internal/srn/` 包对接 SRN 网络。该包是 `github.com/DeliYuxiang/SRNApiClient/srn` 共享模块的门面（facade）层——所有协议逻辑（签名、网络查询、Event 结构）均在 `SRNApiClient` 共享模块中实现，Hijarr 仅通过类型别名和薄包装函数使用它。

```
internal/srn/srn.go      — 门面层（type aliases + 薄包装函数）
internal/srn/provider.go — 三级优先级查询策略
internal/srn/store.go    — 本地 SQLite 队列（srn_queue 表）
```

---

## 节点身份（Ed25519）

每个 Hijarr 实例都有一个 Ed25519 密钥对作为 SRN 网络身份。

**私钥加载优先级（`cmd/hijarr/main.go`）：**

1. `SRN_PRIV_KEY` 环境变量（128 hex chars）— 最高优先级，同时写入数据库
2. `global_state` 数据库中的 `srn_priv_key` 字段
3. 自动生成（WARN 级别警告，生产环境不应使用此模式）

> **生产注意**：若不固化 `SRN_PRIV_KEY`，每次数据库丢失或容器重建都会轮换节点身份。已上传到 relay 的事件将与新身份无关联，维护迁移任务也无法找到旧事件。

私钥加载后通过 `srn.SetNodeKey(priv)` 注册到共享模块，后续所有签名操作自动使用。节点公钥 hex 同时注入 Web 前端（`/api/frontend/config` 中的 `SRNNodePublicKey` 字段）。

---

## 字幕查询：三级优先级

`internal/srn/provider.go` 中的 `Provider.SearchByCacheKey()` 实现三级优先级查询：

### 优先级 1：本地 SQLite 队列
查询 `srn_queue` 表（`internal/srn/store.go`），即本节点本地缓存的待上传或已下载的事件。无网络延迟，适合离线或低延迟场景。

查询逻辑（按 tmdbID/title + season + ep）：
- 有 `tmdbID` → 精确匹配
- 无 `tmdbID` → `LIKE '%title%'` 模糊匹配

### 优先级 2：本地 Backend-SRN（srnfeeder）
当 `BACKEND_SRN_URL` 非空时，向本地 srnfeeder 实例发出 HTTP 查询（调用 `queryOne` → `srnclient.QueryRelay`）。比云端 relay 延迟更低，适合与 srnfeeder 同机或同 Docker 网络部署的场景。

对每个语言（来自 `SRN_PREFERRED_LANGUAGES`）各查询一次，结果通过 `mergeEvents` 去重合并。

### 优先级 3：云端 Relay 网络
当 `SRN_RELAY_URLS` 非空时，调用 `QueryNetworkForLangs` → `srnclient.QueryNetworkForLangs`，向所有配置的 relay 并发查询每个语言，结果去重。

---

## 缓存 Key 格式

SRN Provider 使用 `"title|T<tmdbID>|S<season>|E<ep>"` 格式作为查询 key，由 `ParseCacheKey()` 解析：

- 新格式（preferred）：`"Breaking Bad|T1396|S1|E1"` — 包含 `|T<tmdbID>` 段
- 旧格式（legacy）：`"Breaking Bad|S1|E1"` — 无 tmdbID，回退到 title 模糊匹配

---

## 本地 SQLite 队列（srn_queue 表）

`Store`（`internal/srn/store.go`）管理 `srn_queue` 表，包含：

| 字段 | 类型 | 说明 |
|:---|:---|:---|
| `id` | TEXT PK | Event ID（SHA256 hex，V2 = 64 chars） |
| `event_json` | TEXT | 完整 Event JSON |
| `content` | BLOB | 字幕文件内容 |
| `priv_key` | TEXT | hex 编码的私钥（用于重试上传） |
| `created_at` | INTEGER | Unix 时间戳 |
| `attempts` | INTEGER | 上传尝试次数 |
| `last_error` | TEXT | 最近一次错误信息 |
| `next_retry_at` | INTEGER | 下次重试时间（Unix ts），0=立即可调度 |
| `tmdb_id` | VIRTUAL TEXT | 从 `event_json` 提取（tags[0][1]） |
| `title` | VIRTUAL TEXT | 从 `event_json` 提取（tags[1][1]） |
| `lang` | VIRTUAL TEXT | 从 `event_json` 提取（tags[2][1]） |
| `season` | VIRTUAL INTEGER | 从 `event_json` 提取（tags[3][1]） |
| `ep` | VIRTUAL INTEGER | 从 `event_json` 提取（tags[4][1]） |

**Tag 顺序约定（tags 数组索引必须稳定）：**
```json
[["tmdb","123"], ["title","剧集名"], ["language","zh-hans"], ["s","1"], ["ep","2"]]
```

---

## 事件类型（Event Kinds）

Hijarr 目前使用以下 Kind：

| Kind | 说明 | 相关函数 |
|:---|:---|:---|
| 1001 | 字幕发布（KindSubtitle） | `PublishToNetwork` |
| 1002 | 字幕撤销（KindRetract） | `RetractEvent` |
| 1003 | 字幕替换（KindReplace） | `ReplaceEvent` |

Kind 的具体定义在 `srnrelay` 共享模块中。

---

## 上传流程

字幕事件的完整生命周期：

```
1. 构建 Event（tags 包含 tmdb/title/language/s/ep/filename 等）
2. srn.PublishToNetwork(ev, data, privKey)
   → srnclient.PublishToNetwork → 向所有 SRN_RELAY_URLS 广播
3. 失败时 Store.Enqueue(ev, data, privKey) 入队
4. 下次调度周期 Store.GetTasks(limit) 取出重试
5. 失败 → Store.MarkFailed(id, err, retryAfter) 设置退避延迟
6. 成功 → Store.Remove(id) 从队列删除
```

---

## 下载流程

从 relay 下载字幕内容（`handleApplySubtitle` 与 `processEpisode`）：

```
1. 优先 srnStore.GetContent(eventID) — 本地队列直接返回
2. 回退 srn.DownloadFromRelays(eventID)
   → srnclient.DownloadFromRelays → 向所有 SRN_RELAY_URLS 请求内容
3. 写入 <video_base>.<langTag>.<ext>（O_CREATE|O_EXCL 原子写）
```

---

## 维护迁移：srn-resign-v2

`internal/migrations/srn_resign_v2.go` 是一个一次性 protocol 任务，在 boot 时检查并执行：

**目的**：将本节点本地队列中 V1 短 ID（32 hex = SHA256[:16]）的事件升级为 V2 完整 SHA256 ID（64 hex）。

**流程**：
1. 扫描 `srn_queue` 中属于本节点 pubkey 的所有 Kind 1001 事件
2. 计算 V2 ID（`ev.ComputeIDV2()`）
3. 若 ID 相同（已是 V2）：仅重签并更新 `event_json`
4. 若 ID 变化（V1 → V2）：
   - 向 relay 发布 `Kind 1003` 替换通知（`ReplaceEvent`）
   - 原子更新本地队列（`ReplaceQueueID`）
5. 完成后 `maintenance.Runner` 自动 `syscall.Exec` 重启进程

---

## 语言标签

`SRN_PREFERRED_LANGUAGES` 支持的值（`internal/config/config.go`）：

| 值 | 含义 | Relay 匹配规则 |
|:---|:---|:---|
| `zh` | 任意中文（默认） | 前缀匹配（覆盖 zh-hans/zh-hant/zh-bilingual） |
| `zh-hans` | 简体中文 | 精确匹配 |
| `zh-hant` | 繁体中文 | 精确匹配 |
| `zh-bilingual` | 双语（含中日） | 精确匹配 |
| `en` | 英文 | 精确匹配 |

Relay 侧匹配规则：不含 `-` 的值（如 `zh`）使用 `LIKE 'zh%'` 前缀匹配；含 `-` 的值（如 `zh-hans`）使用 `= 'zh-hans'` 精确匹配。

对应 Sonarr 字幕文件 tag：`zh-hant` → `zh-TW`，其余中文 → `zh`，英文 → `en`。

---

## 相关配置

| 环境变量 | 说明 |
|:---|:---|
| `SRN_RELAY_URLS` | 云端 relay URL 列表（逗号分隔），Priority 3 |
| `BACKEND_SRN_URL` | 本地 srnfeeder URL，Priority 2 |
| `SRN_PREFERRED_LANGUAGES` | 查询语言列表（逗号分隔，有序），默认 `zh` |
| `SRN_PRIV_KEY` | 节点 Ed25519 私钥（128 hex chars），**生产必须固化** |
| `SRN_NODE_ALIAS` | 节点可读代号（可选） |
| `SRN_DB_PATH` | SRN 队列数据库路径，默认同 `CACHE_DB_PATH` |

<!-- doc-sha: c224d156b9ea049f4ba59dc27046a9ef808f1234 -->
