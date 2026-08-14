# 6v520 → 115 离线下载助手

把 [6v520](https://www.6v520.com) 的资源搜出来、勾选磁力链，推到 **115 网盘离线下载**，并按「分类 / 标题(年份) / 第N季」归档。

**不是 2dland 开放平台。** 登录用 115 网页 Cookie（`UID` / `CID` / `SEID`）或扫码，token 存在 `data/token.json`。

仓库：https://github.com/zixiang0520/6v-115

---

## 能做什么

- 搜 6v520，勾选磁力 / http / ed2k，批量推 115 离线
- 自动建目录：`base_dir / 分类 / 标题 (年份) [/ 第N季]`
- 可选 TMDB 规范化标题（走 `api.tmdb.org`）
- 离线任务：列表、批量整理、批量删除、探测已整理
- 文件管理：列目录、移动、删除到回收站（新建文件夹接口目前不稳定）
- Web 管理后台，有访问密码；**不接 QQ 机器人**
- 前端 `go:embed` 打进二进制，改 `web/` 必须重新 `docker compose up -d --build`

---

## 登录 115

设置页粘贴 Cookie，**不要末尾分号**。需要这三个字段：

```
UID=...; CID=...; SEID=...
```

也可以在页面里扫码。Cookie 失效就重新贴或再扫一次。

---

## 快速部署

```bash
git clone https://github.com/zixiang0520/6v-115.git
cd 6v-115
mkdir -p data
# 可选：复制示例后改密码 / Cookie / TMDB
# cp config.example.json data/config.json
docker compose up -d --build
```

浏览器打开 `http://<主机>:8080`（本仓库 NAS 部署用宿主端口 **18900**）。

首次进入：设访问密码 → 填 115 Cookie 或扫码 → 可选 TMDB。

覆盖部署时**不要** `rsync --delete`，并排除 `data/` 和 `.env`，否则会清掉 Cookie 和配置。

```bash
rsync -az --exclude data --exclude .env --exclude .git \
  ./  user@nas:/path/to/6v-to-2dland/
ssh user@nas 'cd /path/to/6v-to-2dland && docker compose up -d --build --force-recreate'
```

验证必须打容器本机，不要只看公网是否通：

```bash
curl -sS http://127.0.0.1:18900/api/health
# {"ok":true}
```

---

## 配置（`data/config.json`）

优先在 Web 设置页改，不要整文件覆盖线上配置。

| 字段 | 默认 | 说明 |
|------|------|------|
| `listen` | `:8080` | 容器内监听 |
| `base_dir` | `6v下载` | 115 里的根目录名 |
| `max_pages` | `8` | 搜索翻页上限 |
| `cookie` | 空 | 115 网页 Cookie，也可只写在 `token.json` |
| `access_password` | 空 | 网页密码，空则走首次向导 |
| `token_file` | `token.json` | 登录态文件 |
| `site_base` | `https://www.6v520.com` | 必须 https |
| `tmdb_api_key` | 空 | 空则不规范化标题 |
| `tmdb_proxy` | 空 | 一般不用；本机 `api.themoviedb.org` 超时就走 `api.tmdb.org` |
| `tmdb_language` | `zh-CN` | TMDB 语言 |

`.gitignore` 已排除 `config.json` / `token.json`，不要提交密钥。

---

## 为什么有的网络会 405

部分出口访问 `webapi.115.com` / `lixian.115.com` 会被 115 WAF 直接打 **405 HTML**。本仓库已改：

| 用途 | 地址 |
|------|------|
| 文件列表 | `https://aps.115.com/natsort/files.php` |
| 路径/移动/删除/回收站 | `https://proapi.115.com/android/...` |
| 离线任务 | `https://115.com/web/lixian/?ct=lixian&ac=...` |

页面上如果整页都是 `http 405: <!doctype html>`，就是后端打 115 仍被拦，不是前端字段名问题。

---

## 本地构建

需要 Go 1.25+。

```bash
go test ./...
go build -trimpath -ldflags="-s -w" -o 6v-to-2dland .
```

---

## 主要接口

除 `/api/health` 和部分 `/api/ui/*` 外，需要先登录拿到 Cookie。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/health` | `{"ok":true}` |
| POST | `/api/ui/login` | `{"password":"..."}` |
| GET | `/api/search?q=` | 搜 6v520 |
| GET | `/api/magnets?url=` | 详情页磁力 |
| POST | `/api/push` | 推离线 |
| GET | `/api/tasks` | 离线任务列表 |
| POST | `/api/tasks/delete` | `{identities, delete_files}` |
| POST | `/api/tasks/organize` | 整理已完成任务 |
| POST | `/api/tasks/probe` | 探测已整理 |
| GET | `/api/files?path=` | 列目录 |
| POST | `/api/files/move` | 移动 |
| POST | `/api/files/delete` | 进回收站 |
| GET/POST | `/api/settings` | 读/写设置 |

---

## 已知限制

- **新建文件夹**：`proapi` 的 add 接口当前返回 404，页面「新建」可能失败；推送归档走的是别的建目录路径。
- **GitHub Actions / GHCR**：当前公开仓库**没有**上传 workflow（本机 token 缺 `workflow` 权限），请本地 `docker compose build`。
- 公网探测 NAS 端口可能被拦，排障请 SSH 到机器上打 `127.0.0.1`。

---

## License

仅供个人学习使用。请遵守 6v520 与 115 的服务条款，下载内容请自行确保有权使用。
