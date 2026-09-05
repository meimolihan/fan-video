.PHONY: all build build-lite build-full build-server build-server-full build-web run run-full dev dev-full dev-server dev-server-full dev-web clean docker docker-full docker-stop tidy sync-pwa sync-webdist

VERSION ?= $(shell git describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null | sed 's/^v//' || echo 0.1.0)
GO_VERSION_PKG := github.com/fan-video/fan-video/internal/version.Version
GO_LDFLAGS := -s -w -X $(GO_VERSION_PKG)=$(VERSION)
DEV_SERVER_PORT ?= 28888
DEV_WEB_PORT ?= 28889
DEV_API_PROXY ?= http://localhost:$(DEV_SERVER_PORT)

# Nowen Video 正式版默认构建入口。
all: build

build: build-web build-server

# 兼容旧脚本：Lite 已正式扶正为 Nowen Video，不再作为独立产品版本。
build-lite: build

# 旧版完整服务仅保留迁移、回滚和兼容验证，不作为正式发行版本。
build-full: build-web build-server-full

# 保持 go:embed 内嵌的 PWA 资源（internal/pwa）与前端源文件同步。
# 前端 PWA 文件（web/public/assets/sw.js、manifest.json）会被 Vite 复制进 dist，
# 而运行时间是走二进制内嵌版本，因此后端构建前必须先同步，避免内嵌内容过期。
.PHONY: sync-pwa
sync-pwa:
	@mkdir -p internal/pwa
	@cp web/public/assets/sw.js internal/pwa/sw.js
	@cp web/public/assets/manifest.json internal/pwa/manifest.json

# 将前端构建产物同步到 internal/embedded/dist，供 go:embed 内嵌进二进制。
# 这样 Release 二进制自包含：即便部署机器上没有任何磁盘 web/dist，
# server-lite 也能回退到内嵌副本提供页面（见 internal/embedded.Resolve）。
.PHONY: sync-webdist
sync-webdist:
	@mkdir -p internal/embedded/dist
	@cp -rf web/dist/. internal/embedded/dist/

build-server:
	@$(MAKE) -s sync-pwa sync-webdist
	@CGO_ENABLED=1 NOWEN_VERSION=$(VERSION) go build -ldflags "$(GO_LDFLAGS)" -o bin/fan-video ./cmd/server-lite

build-server-full:
	CGO_ENABLED=1 NOWEN_VERSION=$(VERSION) go build -ldflags "$(GO_LDFLAGS)" -o bin/fan-video-full ./cmd/server

build-web:
	cd web && VITE_APP_VERSION=$(VERSION) npm run build
	@$(MAKE) -s sync-webdist

# 默认开发模式运行 Nowen Video 正式服务端。
# cmd/server-lite 暂作为内部稳定实现路径保留，避免破坏数据库迁移与回滚链路；
# 它不再代表一个对外的 Lite 产品版本。
# Go 服务直接读取 web/dist，因此每次启动前必须重建当前分支前端。
dev: build-web
	@$(MAKE) -s sync-pwa sync-webdist
	@NOWEN_APP_PORT=$(DEV_SERVER_PORT) NOWEN_DEBUG=true NOWEN_VERSION=$(VERSION) go run -ldflags "$(GO_LDFLAGS)" ./cmd/server-lite

# 旧版完整服务，仅用于兼容验证与必要回滚。
dev-full: build-web
	NOWEN_APP_PORT=$(DEV_SERVER_PORT) NOWEN_DEBUG=true NOWEN_VERSION=$(VERSION) go run -ldflags "$(GO_LDFLAGS)" ./cmd/server

# 仅供明确需要复用现有 dist 的后端调试场景使用。
# 常规开发请使用 make dev。
dev-server:
	@$(MAKE) -s sync-pwa sync-webdist
	@NOWEN_APP_PORT=$(DEV_SERVER_PORT) NOWEN_DEBUG=true NOWEN_VERSION=$(VERSION) go run -ldflags "$(GO_LDFLAGS)" ./cmd/server-lite

dev-server-full:
	NOWEN_APP_PORT=$(DEV_SERVER_PORT) NOWEN_DEBUG=true NOWEN_VERSION=$(VERSION) go run -ldflags "$(GO_LDFLAGS)" ./cmd/server

dev-web:
	cd web && WEB_PORT=$(DEV_WEB_PORT) VITE_API_PROXY_TARGET=$(DEV_API_PROXY) VITE_APP_VERSION=$(VERSION) npm run dev

run: build
	./bin/fan-video

run-full: build-full
	./bin/fan-video-full

docker:
	docker-compose up --build -d

# 旧版兼容镜像，不作为正式发行镜像。
docker-full:
	docker build -f Dockerfile.full -t fan-video:legacy .

docker-stop:
	docker-compose down

clean:
	rm -rf bin/
	rm -rf cache/transcode/
	rm -rf internal/embedded/dist/
	cd web && rm -rf dist/ node_modules/

install-web:
	cd web && npm install

tidy:
	go mod tidy
