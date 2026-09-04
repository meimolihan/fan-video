# fan-video 构建 · 推送 · 运行指南

本文档说明本项目的二进制文件如何**生成**、如何**推送到远程仓库**、如何**运行**，以及日常维护的**常用命令**。

> 项目结构速览：
> - `cmd/server-lite` —— 正式服务入口（唯一发行目标）
> - `web/` —— React + Vite 前端，构建产物由 Go `go:embed` 内嵌进二进制
> - `internal/pwa` —— 前端 PWA 文件（`sw.js`、`manifest.json`）的内嵌副本，构建前必须同步
> - `bin/fan-video` —— 本地构建出来的正式二进制

---

## 一、环境要求

| 依赖 | 版本要求 | 说明 |
|------|----------|------|
| Go | 1.25+ | `go.mod` 声明 `go 1.25.0`，本机位于 `/usr/local/go/bin/go`（可能不在 PATH） |
| Node.js | **>= 20** | 本机当前为 v18，**构建前端前需升级**（用 nvm 或系统包管理器） |
| npm | 与 Node 配套 | 前端依赖安装用 `npm ci` |
| make | 任意 | 提供全部构建/运行入口 |
| docker | 可选 | 仅 Docker 镜像构建/发布时需要 |

---

## 二、二进制如何生成（构建）

### 2.1 一键构建（前端 + 后端）

```bash
make build
```

等价于依次执行：

```bash
make build-web     # 先构建前端：cd web && npm run build → web/dist
make build-server  # 再构建后端：go build → bin/fan-video
```

`build-server` 内部会自动：

1. 执行 `make sync-pwa`，把 `web/public/assets/sw.js`、`manifest.json` 同步到 `internal/pwa/`；
2. 以 `CGO_ENABLED=1 go build` 构建 `cmd/server-lite` 输出到 `bin/fan-video`。

### 2.2 只看版本号如何决定

`Makefile` 自动取最近一个 `vX.Y.Z` 的 Git tag 作为版本号：

```make
VERSION ?= $(shell git describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null | sed 's/^v//' || echo 0.1.0)
```

生成的二进制用 `-ldflags "-s -w -X ...internal/version.Version=$(VERSION)"` 注入版本号。如需手动指定：

```bash
make build VERSION=1.2.1
```

### 2.3 分开构建

```bash
make build-web        # 只构建前端（web/dist）
make build-server     # 只构建后端（自动 sync-pwa + go build）
make build-full       # 旧版完整服务（bin/fan-video-full，仅供兼容验证，非发行版本）
```

### 2.4 手工 go build（不经过 make）

```bash
# 先同步 PWA，否则内嵌资源过期
mkdir -p internal/pwa
cp web/public/assets/sw.js internal/pwa/sw.js
cp web/public/assets/manifest.json internal/pwa/manifest.json

# 正式二进制（本机 Go 路径可能需写明）
/usr/local/go/bin/go build \
  -ldflags "-s -w -X github.com/fan-video/fan-video/internal/version.Version=1.2.1" \
  -o bin/fan-video ./cmd/server-lite
```

### 2.5 验证产物

```bash
./bin/fan-video --help        # 查看可用参数
file bin/fan-video            # 确认平台/架构
ls -lh bin/fan-video
```

> 版权声明：配置模板见 `config.example.yaml`；完整运行时参数可通过 `./bin/fan-video --help` 查看。

---

## 三、如何推送（Git + Docker Hub）

### 3.1 推送代码到 Git 远程

本项目有且仅有一个 origin：`git@github.com:meimolihan/fan-video.git`。

```bash
git add -A
git commit -m "描述本次改动"
git push origin main

# 分支基线说明：
# main                     —— 当前主分支
# refactor/server-lite-v1  —— server-lite 重构基线（同样触发 CI）
```

### 3.2 打版本 tag（release）

```bash
git tag v1.2.1                      # 语义化版本号
git push origin v1.2.1
```

CI（`.github/workflows/server-ci.yml`、`release-contract.yml`）会在 `main` 与
`refactor/server-lite-v1` 分支推送及 PR 时自动运行。

> 注意：`docker-compose.deploy.yml` 中锁定镜像版本时的 tag 为 `cropflre/fan-video:vX.Y.Z`；
> 正式发版 tag 必须与镜像 tag 严格一致。

### 3.3 构建并推送 Docker 镜像（维护者）

官方发布镜像：`cropflre/fan-video`；本机调试镜像：`mobufan/fan-video`。

单架构构建（本机默认架构）：

```bash
# 构建镜像（版本号会覆盖 ARG NOWEN_VERSION）
docker build --build-arg NOWEN_VERSION=1.2.1 -t mobufan/fan-video:latest .

# 推送本机镜像
docker push mobufan/fan-video:latest
```

多架构一键构建 + 推送（amd64 + arm64 + armv7，官方发布用）：

```bash
# 首次需创建并启动 buildx builder
docker buildx create --name nowen-builder --use
docker buildx inspect --bootstrap

# 构建并推送 manifest list（同时打 latest 与版本 tag）
docker buildx build \
  --platform linux/amd64,linux/arm64,linux/arm/v7 \
  -t cropflre/fan-video:latest \
  -t cropflre/fan-video:v1.2.1 \
  --push .

# 验证 manifest
docker buildx imagetools inspect cropflre/fan-video:latest
```

> 注意：Dockerfile 内前端构建使用 `node:20-alpine`，后端使用 `golang:1.25-alpine`，
> 结束镜像为 `alpine:3.24` 并自带 FFmpeg 8.1.2-r0 与硬件解码驱动。
> 前端构建不会使用你本机的 Node，因此**本机 node 版本不影响 Docker 镜像构建**。

### 3.4 飞牛 fnOS `.fpk` 包（可选）

```bash
# 单独打包（模板见 scripts/fpk/）
FPK_VERSION=1.2.1 \
FPK_IMAGE_TAG=v1.2.1 \
DOCKERHUB_REPO=cropflre/fan-video \
node scripts/fpk/build-fpk.mjs

# 产物
#   dist-fpk/fan-video-1.2.1.fpk
#   dist-fpk/SHA256SUMS-fpk.txt
```

`fnpack` 工具会自动下载（`FNPACK_BIN` 可显式指定），也可用 `FNPACK_TOOL_VERSION`
临时覆盖工具版本。详细说明见 `scripts/fpk/README.md`。

---

## 四、如何运行

### 4.1 直接运行二进制（本地/服务器）

```bash
# 先确保前端已构建（web/dist 存在），否则页面为旧资源
make build
# 或直接运行已构建的二进制
./bin/fan-video
```

默认端口 8080、数据目录 `./data`、缓存目录 `./cache`。

可通过环境变量覆盖关键配置：

```bash
NOWEN_APP_PORT=8080 \
NOWEN_APP_DATA_DIR=/path/to/data \
NOWEN_APP_WEB_DIR=/path/to/web/dist \
NOWEN_DATABASE_DB_PATH=/path/to/data/nowen.db \
NOWEN_CACHE_CACHE_DIR=/path/to/cache \
NOWEN_LOGGING_LEVEL=info \
./bin/fan-video
```

完整配置项参考 `config.example.yaml`。

### 4.2 Docker / docker-compose 运行

本机调试栈（`docker-compose.yml`，端口映射 8060:8080，挂载 `/vol2/1000/mydisk/Video:/media:ro`）：

```bash
# 构建并后台启动
docker compose up -d --build

# 查看日志
docker compose logs -f

# 停止（不删数据卷）
docker compose down
```

生产部署范例（`docker-compose.deploy.yml`，拉取官方镜像 `cropflre/fan-video:latest`）：

```bash
docker compose -f docker-compose.deploy.yml up -d
```

> 首次启动后访问 `http://<host>:8080` 注册，第一个注册用户自动成为 admin（超级管理员）。
> 提权/重置密码等数据库操作示例见文末「常用命令」。

### 4.3 开发模式

```bash
make dev          # 构建前端 → 以后端开发模式启动（端口 28888，NOWEN_DEBUG=true）
make dev-web      # 前端热更新（端口 28889，API 代理到 28888）
make dev-server   # 仅后端，复用现有 web/dist
make run          # 构建 + 运行正式二进制（make build 后 ./bin/fan-video）
```

---

## 五、常用命令速查

### 5.1 Makefile 目标

| 命令 | 作用 |
|------|------|
| `make build` | 一键构建正式版本（前端 + 后端 → `bin/fan-video`） |
| `make build-server` | 仅构建后端（自动 sync-pwa） |
| `make build-web` | 仅构建前端 |
| `make build-full` | 构建旧版完整服务（`bin/fan-video-full`） |
| `make dev` | 开发模式启动后端（端口 28888） |
| `make dev-server` | 仅后端开发模式（复用现有 dist） |
| `make dev-web` | 前端开发服务器（端口 28889） |
| `make run` | 构建后直接运行正式二进制 |
| `make run-full` | 构建并运行旧版完整服务 |
| `make docker` | `docker compose up --build -d` |
| `make docker-full` | 构建旧版兼容镜像 `fan-video:legacy` |
| `make docker-stop` | `docker compose down` |
| `make sync-pwa` | 同步 PWA 文件到 `internal/pwa/` |
| `make clean` | 删除 `bin/`、`cache/transcode/`、`web/dist`、`web/node_modules` |
| `make install-web` | `cd web && npm install` |
| `make tidy` | `go mod tidy` |

### 5.2 版本号 bump

```bash
./bump-version.sh 1.2.1
```

脚本会自动更新 `package.json`、`web/package.json` 的 `version` 以及
`Dockerfile` / `Dockerfile.full` 的 `ARG NOWEN_VERSION`，并提示下一步：

```bash
docker compose up -d --build
```

### 5.3 数据与账户运维（容器内 SQLite）

```bash
# 把普通用户提权为 admin
docker exec -it fan-video sqlite3 /data/nowen.db \
  "UPDATE users SET role='admin' WHERE username='yourname';"

# 忘记 admin 密码时，强制下次登录改密
docker exec -it fan-video sqlite3 /data/nowen.db \
  "UPDATE users SET must_change_pwd=1 WHERE role='admin';"

# 数据库路径默认：宿主 <data>/nowen.db
```

### 5.4 前端 lint / 构建校验

```bash
cd web
npm ci            # 安装依赖（锁文件安装）
npm run lint      # ESLint
npm run build     # tsc -b && vite build && verify-retired-ui && audit-ui（含产物校验脚本）
```

---

## 六、常见问题

- **前端版本与二进制不匹配**：未先 `make build-web` 就 `go build`，`go:embed`
  内嵌的 `web/dist` 是旧产物。构建前必须 `make build-web`（或 `make build`）。
- **本机 node 版本过低**：前端构建要求 Node ≥ 20，本机 v18 需先升级。
- **内嵌 PWA 过期**：改过 `web/public/assets/sw.js` / `manifest.json` 后
  未执行 `make sync-pwa` 再构建，运行时报 PWA 生命周期相关行为旧。用 `make build-server`。
- **go build 找不到**：本机 Go 位于 `/usr/local/go/bin/go`，未加入 PATH 时请写明绝对路径，
  或 `export PATH=$PATH:/usr/local/go/bin`。
- **修改了前端样式/CSS**：直接把 CSS 改动合入 `app-ui.css` / `pages-theme.css` 后，
  用 `make dev-web` 前端热更验证，正式发布前跑一遍 `make build-web`。