#!/bin/bash
# ============================================================================
# fan-video 构建 + 推送 Git 远程脚本
#
# 用法:
#   ./scripts/build-and-push.sh <版本号> [选项]
#
# 版本号须为语义化版本，例如:
#   ./scripts/build-and-push.sh 1.2.1
#
# 选项:
#   --yes               跳过推送前确认（默认每步都会询问）
#   --message <msg>     自定义 commit 信息（默认 "release v<版本号>"）
#
# 流程:
#   1. 校验版本号 / 依赖（git、make、go、node）
#   2. 版本号与当前不符时自动 bump（./bump-version.sh）
#   3. make build 构建正式二进制（bin/fan-video）
#   4. git add + commit（构建产物已在 .gitignore 中，不会入库）
#   5. git tag v<版本号>
#   6. git push origin <当前分支> + git push origin v<版本号>
# ============================================================================

set -euo pipefail

# ---------- 颜色 ----------
gl_reset=$'\033[0m'
gl_yellow=$'\033[38;5;11m'
gl_red=$'\033[38;5;9m'
gl_green=$'\033[38;5;10m'
gl_cyan=$'\033[38;5;14m'
gl_bai=$'\033[38;5;15m'

out()   { printf '%b%s%b\n' "$gl_bai" "$*" "$gl_reset"; }
ok()    { printf '%b%s%b\n' "$gl_green" "$*" "$gl_reset"; }
warn()  { printf '%b%s%b\n' "$gl_yellow" "$*" "$gl_reset"; }
err()   { printf '%b%s%b\n' "$gl_red" "$*" "$gl_reset" >&2; }
info()  { printf '%s\n' "$*"; }

usage() {
    out "用法: $0 <版本号> [--yes] [--message \"提交信息\"]"
    out "示例: $0 1.2.1"
    exit 1
}

confirm() {
    # 调用方式: confirm "操作描述"; 返回 0=确认 1=取消
    if [[ "${YES:-0}" == "1" ]]; then
        ok "已确认（--yes）: $1"
        return 0
    fi
    printf '确认%s? [y/N] ' "$1" >&2
    local ans
    read -r ans
    case "$ans" in
        y|Y|yes|YES) return 0 ;;
        *) warn "已取消" >&2; return 1 ;;
    esac
}

ensure_dep() {
    local name="$1"
    if ! command -v "$name" >/dev/null 2>&1; then
        err "缺少依赖: $name"
        case "$name" in
            go)  warn "本机 Go 位于 /usr/local/go/bin，请 export PATH=\$PATH:/usr/local/go/bin" >&2 ;;
            node) warn "前端构建需要 Node.js，参考 BUILD.md 第一节" >&2 ;;
        esac
        exit 1
    fi
}

# ---------- 命令行解析 ----------
VER=""
MSG=""
YES=0
while [[ $# -gt 0 ]]; do
    case "$1" in
        --yes)        YES=1 ;;
        --message)    MSG="${2:?--message 需要参数}"; shift ;;
        -h|--help)    usage ;;
        -*)
            err "未知选项: $1"
            usage
            ;;
        *)
            if [[ -n "$VER" ]]; then
                err "只能指定一个版本号"
                usage
            fi
            VER="$1"
            ;;
    esac
    shift
done

if [[ -z "$VER" ]]; then
    err "缺少版本号参数"
    usage
fi

if ! [[ "$VER" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    err "版本号格式错误，需语义化版本 如 1.2.1"
    exit 1
fi

# ---------- 环境准备 ----------
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
cd "${SCRIPT_DIR}/.."

if ! command -v go >/dev/null 2>&1 && [ -x /usr/local/go/bin/go ]; then
    export PATH="/usr/local/go/bin:$PATH"
fi

ensure_dep git
ensure_dep make
ensure_dep go
ensure_dep node

NODE_MAJOR=$(node -p 'process.versions.node.split(".")[0]')
if (( NODE_MAJOR < 18 )); then
    err "Node.js 版本过低: $(node -v)，需要 >= 18（推荐 >= 20）"
    exit 1
elif (( NODE_MAJOR < 20 )); then
    warn "Node.js 当前为 $(node -v)，推荐 >= 20（见 BUILD.md）"
fi

# ---------- 构建前检查 ----------
TAG="v$VER"
if git rev-parse -q --verify "refs/tags/$TAG" >/dev/null; then
    err "Git tag 已存在: $TAG，请换一个版本号或先删除旧 tag"
    exit 1
fi

BRANCH=$(git rev-parse --abbrev-ref HEAD)
REMOTE=$(git remote get-url origin)

out "══════════════════════════════════════════════════════"
info "版本:    $VER"
info "tag:     $TAG"
info "分支:    $BRANCH"
info "远程:    $REMOTE"
info "Go:      $(go version)"
info "Node:    $(node -v)"
out "══════════════════════════════════════════════════════"

# ---------- 工作区提示 ----------
if [[ -n "$(git status --porcelain)" ]]; then
    warn "检测到未提交的改动，将随本次 release 一起提交："
    git status --porcelain | sed 's/^/    /'
fi

# ---------- 1. bump 版本 ----------
CUR_VER=$(grep -m1 '"version"' package.json | sed 's/.*"version": *"\([^"]*\)".*/\1/' || true)
if [[ "$CUR_VER" != "$VER" ]]; then
    info "版本号 ${CUR_VER:-unknown} -> $VER，执行 bump-version.sh"
    bash ./bump-version.sh "$VER"
else
    ok "版本号已是 $VER，跳过 bump"
fi

# ---------- 2. 构建 ----------
info "开始构建 make build（VERSION=$VER，前端 -> go:embed -> bin/fan-video）..."
make build VERSION="$VER"
ok "构建完成: $(ls -lh bin/fan-video | awk '{print $5}') bits@bin/fan-video"

# ---------- 3. 提交 ----------
git add -A
if git diff --cached --quiet; then
    ok "无需要提交的改动，跳过 commit"
else
    COMMIT_MSG="${MSG:-release v$VER}"
    git commit -m "$COMMIT_MSG"
    ok "已提交: $(git rev-parse --short HEAD) — $COMMIT_MSG"
fi

# ---------- 4. 打 tag ----------
git tag "$TAG"
ok "已打 tag: $TAG"

# ---------- 5. 推送 ----------
out ""
warn "即将推送（不可轻易撤销）："
info "  git push origin $BRANCH"
info "  git push origin $TAG"
if ! confirm "推送到 origin"; then
    out ""
    warn "已保留本地提交与 tag，未推送。可手动执行："
    info "  git push origin $BRANCH $TAG"
    exit 0
fi

git push origin "$BRANCH"
git push origin "$TAG"

out ""
ok "✅ release v$VER 已提交并推送到 origin:"
info "   分支: $BRANCH"
info "   tag:  $TAG"
info "   远程: $REMOTE"