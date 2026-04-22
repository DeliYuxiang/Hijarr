# 内网域名劫持与 MITM 代理配置指南

本指南介绍如何利用 **Caddy** 和 **Hijarr** 协同工作，实现对 Sonarr 元数据的中文化透明劫持。

## 1. 劫持原理

```
Sonarr
    │  DNS 查询 skyhook.sonarr.tv / api.thetvdb.com / api4.thetvdb.com
    ▼
AdGuard Home / extra_hosts (DNS 重写)
    │  返回 Caddy 宿主机 IP
    ▼
Caddy (HTTPS 终点 + 内部自签名证书)
    │  解密 TLS，路径改写：/sonarr{uri}，转发 HTTP 请求
    ▼
Hijarr :8001 (元数据中文化 MITM)
    │  TVDBMitmProxy 处理请求
    │  · 劫持 URL 中的语言段（eng → zho）
    │  · 通过 TMDB API 获取中文标题/集名
    │  · 用 gjson/sjson 原地 patch JSON 响应
    │  · 向真实上游发出请求（使用公网 DNS，绕过 AdGuard）
    ▼
skyhook.sonarr.tv / api.thetvdb.com（真实服务器）
```

**拦截的域名：**
- `skyhook.sonarr.tv` — Sonarr v4 核心元数据源
- `api.thetvdb.com` — TVDB v3/v4 原生 API
- `api4.thetvdb.com` — TVDB v4 备用端点

**Hijarr 支持的 API 格式：**
- Skyhook v1：`/sonarr/v1/tvdb/shows/<id>`，`/sonarr/v1/tvdb/search/<query>`
- TVDB v4：`/sonarr/v4/series/<id>/**`，`/sonarr/v4/search`

> **重要**：Hijarr 容器的 DNS **必须**设置为公网 DNS（如 `8.8.8.8`），**不能**指向 AdGuard Home，否则 Hijarr 向真实上游发出的请求也会被劫持，造成无限循环。

---

## 2. 第一阶段：选择域名拦截方案

你需要将以下域名全部指向 **Caddy 容器所在的宿主机 IP**：
- `skyhook.sonarr.tv`
- `api.thetvdb.com`
- `api4.thetvdb.com`

根据你的环境选择以下任一方案：

### 方案 A：AdGuard Home / DNS 服务器
在 DNS 服务器管理界面添加 **DNS 重写 (DNS Rewrites)**：
- 将上述域名直接指向 Caddy 宿主机 IP。

### 方案 B：Docker Compose `extra_hosts`（推荐）
在 Sonarr 的 `docker-compose.yml` 中直接硬编码：
```yaml
services:
  sonarr:
    # ...
    extra_hosts:
      - "skyhook.sonarr.tv:192.168.x.x"  # Caddy 宿主机 IP
      - "api.thetvdb.com:192.168.x.x"
      - "api4.thetvdb.com:192.168.x.x"
```

### 方案 C：Volume 挂载覆盖 `/etc/hosts`
```yaml
services:
  sonarr:
    volumes:
      - ./my_hosts:/etc/hosts:ro
```
`my_hosts` 内容：
```text
127.0.0.1 localhost
192.168.x.x skyhook.sonarr.tv
192.168.x.x api.thetvdb.com
192.168.x.x api4.thetvdb.com
```

### 方案 D：构建自定义 Sonarr 镜像（告别复杂配置）
创建 `Dockerfile.sonarr`：
```dockerfile
FROM linuxserver/sonarr:latest

# 注入启动脚本，自动下载并信任 Caddy 根证书
RUN echo '#!/bin/bash \n\
echo "正在从 Caddy 获取根证书..." \n\
curl -sk https://skyhook.sonarr.tv/cert/root.crt -o /usr/local/share/ca-certificates/caddy-root.crt \n\
update-ca-certificates \n\
exec /init' > /usr/local/bin/entrypoint-zh.sh && \
chmod +x /usr/local/bin/entrypoint-zh.sh

ENTRYPOINT ["/usr/local/bin/entrypoint-zh.sh"]
```
```bash
docker build -t sonarr-zh -f Dockerfile.sonarr .
```

---

## 3. 第二阶段：Caddy 配置（SSL & 代理）

配置 `Caddyfile`。Caddy 会自动生成内部 CA 并颁发证书。

```caddy
api.thetvdb.com, api4.thetvdb.com, skyhook.sonarr.tv {
    # 使用 Caddy 内部自签名 CA
    tls internal

    # 暴露根证书下载路径（供客户端一键信任）
    handle_path /cert* {
        root * /data/caddy/pki/authorities/local
        file_server
    }

    # 转发给 Hijarr
    handle {
        # 基于域名匹配作前缀重写（适配 Hijarr Gin 路由 /sonarr/*）
        @sonarr host skyhook.sonarr.tv api.thetvdb.com api4.thetvdb.com
        rewrite @sonarr /sonarr{uri}

        reverse_proxy 192.168.x.x:8001 {  # 替换为 Hijarr 宿主机 IP
            header_up Host {host}
            header_up X-Real-IP {remote_host}
        }
    }
}
```

> **注意**：若使用 Docker Compose 且 Caddy 与 Hijarr 在同一网络，可将 IP 替换为容器名：`reverse_proxy hijarr:8001`。

**路由行为说明（`internal/proxy/tvdb.go`）：**
- Caddy 将所有请求重写为 `/sonarr{uri}` 并转发给 Hijarr
- Hijarr 的 `/sonarr/*` 路由匹配 `TVDBMitmProxy`
- 若路径以 `/api` 开头（即 `/sonarr/api...`），`TVDBMitmProxy` 自动转发给 `TorznabProxy`（Prowlarr 代理）

---

## 4. 第三阶段：客户端信任根证书

Sonarr 容器默认不信任 Caddy 的自签名证书，必须在容器启动时安装根证书。

### Sonarr 配置示例
```yaml
services:
  sonarr:
    image: linuxserver/sonarr:latest
    entrypoint: >
      /bin/sh -c "
      echo '正在从 Caddy 获取根证书并注入信任库...' &&
      curl -sk https://skyhook.sonarr.tv/cert/root.crt -o /usr/local/share/ca-certificates/caddy-root.crt &&
      update-ca-certificates &&
      exec /init"
    dns:
      - 192.168.x.x  # AdGuard Home，使 skyhook.sonarr.tv 解析到 Caddy
    extra_hosts:
      - "skyhook.sonarr.tv:192.168.x.x"
      - "api.thetvdb.com:192.168.x.x"
      - "api4.thetvdb.com:192.168.x.x"
```

> **提示**：`curl -sk` 中的 `-k` 仅用于首次下载根证书时绕过证书校验；证书安装后后续所有 HTTPS 请求均正常校验。

### Hijarr 配置（防止 DNS 循环）
```yaml
services:
  hijarr:
    # ...
    dns:
      - 8.8.8.8  # 公网 DNS，避免 Hijarr 自身被劫持形成循环
    environment:
      - TMDB_API_KEY=your_tmdb_api_key
      - TARGET_LANGUAGE=zh-CN
      - TVDB_LANGUAGE=zho
```

---

## 5. 第四阶段：验证

### 验证元数据劫持（Sonarr）
```bash
# 检查响应头是否经过 Caddy
docker exec sonarr curl -skI https://skyhook.sonarr.tv/

# 检查中文标题是否注入（替换 TVDB ID）
docker exec sonarr curl -sk "https://skyhook.sonarr.tv/v1/tvdb/shows/12345" | python3 -m json.tool | grep -E '"title"'
```
预期：响应头包含 `via: 1.1 Caddy`，`title` 字段为中文。

### 验证 Hijarr 日志
```bash
docker logs hijarr --tail 50 | grep -E "TVDB|Skyhook|TMDB|劫持|翻译"
```

预期：出现类似 `💀 [Skyhook 详情] ID: 12345 已翻译` 或 `💀 [TVDB 劫持] ID: ...` 的日志行。

> **注意**：这些日志为 DEBUG 级别，需设置 `LOG_LEVEL=info,proxy=debug` 才可见。

### 在 Sonarr UI 中验证
对已有剧集点击 **Refresh & Scan**，剧名和集标题应更新为中文。

---

## 常见问题排查

| 问题 | 原因 | 解决方案 |
| :--- | :--- | :--- |
| DNS 无限循环 | Hijarr 自身 DNS 指向了 AdGuard Home | 在 Hijarr 容器的 `docker-compose.yml` 中设置 `dns: - 8.8.8.8` |
| Sonarr UI 没有变化 | 劫持仅对新操作生效 | 对已有剧集点击 **Refresh & Scan** |
| SSL 证书错误 | Sonarr 未安装 Caddy 根证书 | 确认第三阶段的 entrypoint 脚本已成功执行 `update-ca-certificates` |
| 元数据仍为英文 | TMDB API Key 未配置或 Key 无效 | 检查 Hijarr 容器的 `TMDB_API_KEY` 环境变量 |
| 集标题无中文但剧名有 | TMDB 无该剧的集标题翻译 | 正常现象，部分冷门剧集 TMDB 缺少中文集标题 |
| `api4.thetvdb.com` 不生效 | 遗漏了此域名的劫持配置 | 确认 Caddyfile 中包含 `api4.thetvdb.com`，DNS 重写中也有此域名 |
| Prowlarr 请求被错误路由 | `/api` 路径被 TVDBMitmProxy 捕获 | 正常设计：`/sonarr/api*` 由 `TVDBMitmProxy` 自动转发给 `TorznabProxy` |

<!-- doc-sha: c224d156b9ea049f4ba59dc27046a9ef808f1234 -->
