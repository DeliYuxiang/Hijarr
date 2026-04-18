# 内网域名劫持与 MITM 代理配置指南

本指南介绍如何利用 **Caddy** 和 **Lagarr** 协同工作，实现对 Sonarr/Bazarr 的元数据与字幕搜索中文化劫持。

## 1. 劫持原理

```
Sonarr/Bazarr
    │  DNS 查询 skyhook.sonarr.tv / api.assrt.net
    ▼
AdGuard Home (DNS 重写)
    │  返回 Caddy 宿主机 IP
    ▼
Caddy (HTTPS 终点 + 自签名证书)
    │  解密 TLS，转发 HTTP 请求
    ▼
Lagarr :8000 (数据篡改 / 聚合)
    │  向真实上游发出请求（使用 8.8.8.8，绕过 AdGuard）
    ▼
skyhook.sonarr.tv / api.thetvdb.com / api.assrt.net（真实服务器）
```

- **拦截层**：将目标域名（Skyhook/TVDB/Assrt）指向 Caddy。
- **SSL 层 (Caddy)**：提供自签名证书并作为 HTTPS 终点，解决证书信任问题。
- **逻辑层 (Lagarr)**：接收 Caddy 转发的请求，执行数据篡改（翻译/聚合）。
- **客户端 (Sonarr/Bazarr)**：信任 Caddy 颁发的根证书，从而透明地接收劫持后的中文数据。

> ⚠️ **重要**：Lagarr 容器的 DNS 必须设置为公网 DNS（如 `8.8.8.8`），**不能**指向 AdGuard Home，否则 Lagarr 向真实上游发出的请求也会被劫持，造成无限循环。

---

## 2. 第一阶段：选择域名拦截方案

你需要将以下域名全部指向 **Caddy 容器所在的宿主机 IP**：
- `skyhook.sonarr.tv`（Sonarr v4 核心元数据）
- `api.thetvdb.com`、`api4.thetvdb.com`（TVDB 原生 API）
- `api.assrt.net`（字幕搜索增强，可选）

> **注意**：`file1.assrt.net`（字幕文件下载域名）**无需劫持**。Lagarr 会自动将字幕详情响应中的下载链接改写为经由自身代理的地址，由 Lagarr 容器直接访问该域名并把文件转发给 Bazarr。

根据你的环境选择以下任一方案：

### 方案 A：AdGuard Home / DNS 服务器（推荐）
在 DNS 服务器管理界面添加 **DNS 重写 (DNS Rewrites)**：
- 将上述域名直接指向 Caddy 宿主机 IP。

### 方案 B：Docker Compose `extra_hosts`
在 Sonarr/Bazarr 的 `docker-compose.yml` 中直接硬编码：
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
192.168.x.x api.assrt.net
```

### 方案 D：构建自定义镜像（最推荐，告别复杂配置）
创建 `Dockerfile.bazarr`：
```dockerfile
FROM linuxserver/bazarr:latest

# 强制 Python 及其库使用系统证书库
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt

# 注入启动脚本，自动下载并信任 Caddy 根证书
RUN echo '#!/bin/bash \n\
echo "正在从 Caddy 获取根证书..." \n\
curl -sk https://api.assrt.net/cert/root.crt -o /usr/local/share/ca-certificates/caddy-root.crt \n\
update-ca-certificates \n\
exec /init' > /usr/local/bin/entrypoint-zh.sh && \
chmod +x /usr/local/bin/entrypoint-zh.sh

ENTRYPOINT ["/usr/local/bin/entrypoint-zh.sh"]
```
```bash
docker build -t bazarr-zh -f Dockerfile.bazarr .
```

---

## 3. 第二阶段：Caddy 配置（SSL & 代理）

配置 `Caddyfile`。Caddy 会自动生成内部 CA 并颁发证书。

```caddy
skyhook.sonarr.tv, api.thetvdb.com, api4.thetvdb.com, api.assrt.net {
    # 启用内部自签名 CA
    tls internal

    # 证书分发：方便客户端下载根证书
    handle_path /cert* {
        root * /data/caddy/pki/authorities/local
        file_server
    }

    # ==========================
    # 路径重写 (适配 Hijarr Gin 路由)
    # ==========================
    @sonarr host skyhook.sonarr.tv api.thetvdb.com api4.thetvdb.com
    rewrite @sonarr /sonarr{uri}

    @bazarr host api.assrt.net
    rewrite @bazarr /bazarr{uri}

    # 转发给 Hijarr
    handle {
        reverse_proxy hijarr:8001 {
            header_up Host {host}
            header_up X-Real-IP {remote_host}
        }
    }
}
```

---

## 4. 第三阶段：客户端信任根证书

容器（如 Sonarr/Bazarr）默认不信任 Caddy 的自签名证书，必须在容器启动时安装根证书。

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
```

### Bazarr 配置示例
```yaml
services:
  bazarr:
    image: linuxserver/bazarr:latest
    entrypoint: >
      /bin/sh -c "
      echo '正在从 Caddy 获取根证书并注入信任库...' &&
      curl -sk https://api.assrt.net/cert/root.crt -o /usr/local/share/ca-certificates/caddy-root.crt &&
      update-ca-certificates &&
      exec /init"
```

---

## 5. 第四阶段：验证

### 验证元数据劫持（Sonarr）
```bash
docker exec sonarr curl -skI https://skyhook.sonarr.tv/
```
预期：响应头包含 `via: 1.1 Caddy`，无 SSL 报错。

### 验证字幕搜索劫持（Bazarr）
```bash
# 搜索字幕，应返回中文翻译后的查询结果
docker exec bazarr curl -sk "https://api.assrt.net/v1/sub/search?token=YOUR_TOKEN&q=Inside+Out+2015&is_file=1"
```
预期：Lagarr 日志出现 `🎯 [Assrt 劫持]` 行，返回中文剧名对应的结果。

### 验证字幕文件下载（关键）
查看 Bazarr 日志，字幕下载成功后应不再出现如下错误：
```
Failed to resolve 'file1.assrt.net'
```
改为出现 Lagarr 日志：
```
⬇️  [文件代理] 下载: http://file1.assrt.net/onthefly/...
```

---

## 💡 常见问题排查

| 问题 | 原因 | 解决方案 |
| :--- | :--- | :--- |
| DNS 无限循环 | Lagarr 自身 DNS 指向了 AdGuard Home | 在 Lagarr 容器的 `docker-compose.yml` 中设置 `dns: - 8.8.8.8` |
| Sonarr UI 没有变化 | 劫持仅对新操作生效 | 对已有剧集点击 **Refresh & Scan** |
| Bazarr 搜索失败 | `api.assrt.net` 未被劫持或证书未安装 | 检查 DNS 重写规则，并确认 Bazarr 已完成第三阶段的证书安装 |
| 字幕下载失败（`file1.assrt.net`）| Bazarr 绕过 Lagarr 直连下载域名 | 确认使用最新版 Lagarr（已内置文件下载代理），无需额外劫持 `file1.assrt.net` |
| 字幕缓存未命中 | 缓存存储在内存中，重启后清空 | 正常现象，重启后首次查询会重新拉取并缓存 |
