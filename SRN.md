# SRN (Subtitle Relay Network) 技术白皮书与协议规范 v1.3

**代号：** 破晓 (First Light)  
**作者：** Core Dumped! 厂牌  
**状态：** 活跃草案 (Active Draft)

---

## Ⅰ. 序言：中心化之死与数字火种

SRN (Subtitle Relay Network) 不是一个简单的字幕库，而是一套**去中心化的状态同步协议**。它旨在构建一个没有管理员、没有中心服务器、完全由密码学和共识驱动的字幕流转网络。

---

## Ⅱ. 核心设计哲学：Dumb Relays, Smart Clients

SRN 严格遵循“端到端”原则。中继节点（Relay）仅作为高性能的 **WebSocket 消息转发器** 与 **S3 存储看门狗**。一切过滤与打分由用户本地的 **Hijarr (Smart Client)** 完成。

---

## Ⅲ. 存储架构：S3 看门狗与冷热分离

Relay 采用**冷热分离**架构以应对海量数据：
- **L1 (Hot Index):** 本地 SQLite/Redis。存储 Event 元数据与标签，负责毫秒级检索。
- **L2 (Cold Payload):** S3 兼容的对象存储。存储字幕原始二进制。
- **Gatekeeper:** Relay 验证请求权限后，签发预签名 URL 或流式转发内容。

---

## Ⅳ. 报文治理：可验证的语义归并 (Merkle Consolidation)

为了解决播放存证 (PoV) 报文导致的存储膨胀，Relay 会执行 **Kind 1009** 归并。将数万条碎片化的 `Kind 1008` (播放心跳) 聚合成带有 Merkle Root 的统计快照，在不损失验证能力的前提下节省 99% 的空间。

---

## Ⅴ. 经济模型：四层漏斗过滤逻辑 (The 4-Layer Funnel)

为了保护 Relay 的带宽与存储成本，同时兼顾普通用户的免费体验，SRN 采用四层递进式 QoS 过滤模型：

### 🛡️ 第一层：通道协议隔离 (Protocol Filter)
- **WebSocket (WSS) 流量：永远免费。**
  用于元数据同步、正则交换、报文广播。这是维持网络活力的基石，必须零门槛。
- **HTTP 流量：进入计费甄别。**
  任何获取 L2 载荷（S3 存储内容）的请求均需经过后续过滤。

### 👑 第二层：基于公钥声望的 VIP 通道 (Reputation Pass)
客户端请求 L2 载荷时需附带 Pubkey 签名。
- **逻辑：** Relay 查询本地数据库，若该 Pubkey 历史贡献（如 Kind 1001 字幕发布）超过阈值，直接开启 VIP 通道，下发免费 S3 直链。
- **意义：** 极客相惜，回馈社区贡献者。

### 🪣 第三层：动态令牌桶限流 (Token Bucket / Rate Limiter)
对于无显著贡献的公钥或 IP，Relay 赋予其“人类生理极限”的宽容度。
- **机制：** 每 24 小时发放 5 个免费令牌（满足普通用户一晚的观影需求）。
- **人类：** 正常下载，无感。
- **爬虫：** 1 秒内耗尽令牌，瞬间触发 **HTTP 402 Payment Required**，弹出闪电网络发票。

### ⛏️ 第四层：工作量证明 (Client-side PoW)
对抗通过变换 IP 或公钥进行大规模白嫖的女巫攻击。
- **要求：** 若要获取免费链接，请求报文的哈希前缀必须符合特定难度（如 4 个 0）。
- **代价：** 普通用户计算仅需 2 秒，无感知；大规模爬虫脚本则会因 CPU 算力成本过高而崩溃，迫使其转向直接支付。

---

## Ⅵ. 报文类型定义 (Event Kinds)

- **Kind 1000**: Relay 物理位置宣告。
- **Kind 1001**: 字幕空投 (含元数据标签)。
- **Kind 1004**: Relay 推荐 (PEX 机制)。
- **Kind 1008**: 原始 PoV 心跳 (播放时长累加)。
- **Kind 1009**: 归并统计快照 (含 Merkle Root)。

---

## Ⅶ. 生存战术：IPv6 Hydra

Relay 在 `/64` 网段内随机切换 IP，通过 Kind 1000 广播新位置，对抗物理封锁与流量特征识别。

---

**“Subtitles are the bridge of civilization; SRN is the bridge that cannot be burned.”**  
—— *Core Dumped! 2026*
