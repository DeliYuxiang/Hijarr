# 内网域名劫持与 MITM 代理配置指南

本指南介绍如何利用 **Caddy** 和 **Hijarr** 协同工作，实现对 Sonarr 元数据的中文化透明劫持。

## 1. 劫持原理

```
Sonarr
    │  DNS 查询 skyhook.sonarr.tv / api.thetvdb.com
    ▼
AdGuard Home (DNS 重写)
    │  返回 Caddy 宿主机 IP
    ▼
Caddy (HTTPS 终点 + 自签名证书)
    │  解密 TLS，转发 HTTP 请求（路径改写：/sonarr{uri}）
    ▼
Hijarr :8001 (元数据中文化 MITM)
    │  向真实上游发出请求（使用公网 DNS，绕过 AdGuard）
    ▼
skyhook.sonarr.tv / api.thetvdb.com（真实服务器）
```

- **拦截层**：将目标域名（Skyhook/TVDB）指向 Caddy。
- **SSL 层 (Caddy)**：提供自签名证书并作为 HTTPS 终点，解决证书信任问题。
- **逻辑层 (Hijarr)**：接收 Caddy 转发的请求，用 TMDB 中文数据实时改写元数据（`gjson/sjson` patch）。
- **客户端 (Sonarr)**：信任 Caddy 颁发的根证书，从而透明地接收劫持后的中文元数据。

> ⚠️ **重要**：Hijarr 容器的 DNS 必须设置为公网 DNS（如 `8.8.8.8`），**不能**指向 AdGuard Home，否则 Hijarr 向真实上游发出的请求也会被劫持，造成无限循环。

---

## 2. 第一阶段：选择域名拦截方案

你需要将以下域名全部指向 **Caddy 容器所在的宿主机 IP**：
- `skyhook.sonarr.tv`（Sonarr v4 核心元数据）
- `api.thetvdb.com`、`api4.thetvdb.com`（TVDB 原生 API）

根据你的环境选择以下任一方案：

### 方案 A：AdGuard Home / DNS 服务器（推荐）
在 DNS 服务器管理界面添加 **DNS 重写 (DNS Rewrites)**：
- 将上述域名直接指向 Caddy 宿主机 IP。

### 方案 B：Docker Compose `extra_hosts`
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

### 方案 D：构建自定义镜像（告别复杂配置）
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
```

> **提示**：`curl -sk` 中的 `-k` 仅用于首次下载根证书时绕过证书校验；证书安装后后续所有 HTTPS 请求均正常校验。

---

## 5. 第四阶段：验证

### 验证元数据劫持（Sonarr）
```bash
# 检查响应头是否经过 Caddy
docker exec sonarr curl -skI https://skyhook.sonarr.tv/

# 检查中文标题是否注入（替换 TVDB ID）
docker exec sonarr curl -sk "https://skyhook.sonarr.tv/v3/shows/12345" | python3 -m json.tool | grep -E '"title"|"overview"'
```
预期：响应头包含 `via: 1.1 Caddy`，`title` 字段为中文。

### 验证 Hijarr 日志
```bash
docker logs hijarr --tail 50 | grep -E "TVDB|sonarr|TMDB"
```
预期：出现类似 `🎯 [proxy] TVDB patch: ...` 的日志行。

### 在 Sonarr UI 中验证
对已有剧集点击 **Refresh & Scan**，剧名和集标题应更新为中文。

---

## 💡 常见问题排查

| 问题 | 原因 | 解决方案 |
| :--- | :--- | :--- |
| DNS 无限循环 | Hijarr 自身 DNS 指向了 AdGuard Home | 在 Hijarr 容器的 `docker-compose.yml` 中设置 `dns: - 8.8.8.8` |
| Sonarr UI 没有变化 | 劫持仅对新操作生效 | 对已有剧集点击 **Refresh & Scan** |
| SSL 证书错误 | Sonarr 未安装 Caddy 根证书 | 确认第三阶段的 entrypoint 脚本已成功执行 `update-ca-certificates` |
| 元数据仍为英文 | TMDB API Key 未配置或 Key 无效 | 检查 Hijarr 容器的 `TMDB_API_KEY` 环境变量 |
| 字幕缓存未命中 | 缓存存储在内存/SQLite 中，重启后首次查询需重新拉取 | 正常现象，重启后等待首次同步完成 |
