# 部署指南

6v520 → **115** 离线助手。单容器，前端已 `go:embed`。镜像不含密钥，配置和 Cookie 只放挂载目录。

## 1. 准备

```bash
mkdir -p ./data
# 可不先建 config.json，首次打开网页会向导
# 若手写：
cp config.example.json ./data/config.json
```

`data/config.json` 里填：

- `access_password`：网页密码
- `cookie`：115 网页 Cookie（`UID=...; CID=...; SEID=...`，不要末尾分号）
- `tmdb_api_key` / `tmdb_proxy`：可选

Cookie 也可以只写在 `data/token.json` 的 `cookie` 字段。

## 2. Docker Compose

```bash
docker compose up -d --build --force-recreate
```

默认容器内 `:8080`。宿主端口用 `.env` 的 `HOST_PORT` 覆盖，例如：

```
HOST_PORT=18900
```

对应 `18900:8080`。

**改了 `web/` 必须 rebuild。** 只 rsync 前端文件进宿主机，运行中的容器不会变。

## 3. 覆盖已有部署

同步源码时排除数据和配置，不要 `--delete`：

```bash
rsync -az --exclude data --exclude .env --exclude .git \
  ./  user@host:/path/to/app/
ssh user@host 'cd /path/to/app && docker compose up -d --build --force-recreate'
```

## 4. 验证

在**运行容器的那台机器**上：

```bash
curl -sS http://127.0.0.1:${HOST_PORT:-8080}/api/health
# 期望 {"ok":true}
```

登录后再打 `/api/tasks`、`/api/files?path=/`，正文必须是 JSON，不能是 `405` 或 `<!doctype html>`。

公网端口可能被拦，公网 curl 失败不能当「服务挂了」。

## 5. 反向代理（可选）

```nginx
server {
    listen 443 ssl;
    server_name 115-helper.example.com;
    location / {
        proxy_pass http://127.0.0.1:18900;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

## 6. TMDB

- 优先直连 `api.tmdb.org`。部分网络 `api.themoviedb.org` 会超时。
- 容器里不要填 `127.0.0.1` 当代理，那是容器自己。
- 本仓库实测：经部分 HTTP 代理 CONNECT 后再 TLS 会断，TMDB 不要硬走那种代理。
- TMDB 挂了只是标题不规范化，不影响推 115。

## 7. 常见问题

| 现象 | 原因 / 处理 |
|------|-------------|
| 文件页 / 任务页整页 `http 405: <!doctype` | 115 WAF。应走 natsort / proapi / `115.com/web/lixian`，旧 `webapi.115.com` 会中招 |
| 刷新页面还是旧 UI | 前端在二进制里。必须 `--build --force-recreate`，浏览器强制刷新 |
| Cookie 登录后仍未登录 | Cookie 要含 UID/CID/SEID，去掉末尾 `;` |
| 新建文件夹失败 | 当前 android `files/add` 返回 404，已知限制 |
| 容器起来又退出 | `docker logs`；检查 `data/config.json` 是否合法 JSON |
| 升级丢登录 | 挂载丢了或 rsync 覆盖了 `data/` |

## 8. 从源码跑（不经过 Docker）

工作目录要能写到 `data/`（或你在 config 里写的路径）：

```bash
mkdir -p data
go run .
```
