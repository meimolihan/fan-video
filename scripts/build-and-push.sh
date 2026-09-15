#!/bin/bash
#
# fan-video (Nowen Video) - 发布脚本（触发 GitHub Actions 自动构建）
# 不在本地编译任何产物：仅更新版本号、推送代码并打 v 开头 tag。
# 推送 tag 后由 GitHub Actions 自动完成全部编译与发布（单条 workflow run）：
#   release.yml -> amd64/arm64 自包含二进制（内嵌前端 dist/PWA）创建 GitHub Release
#                  + multi-arch Docker 镜像（latest + 版本标签，NOWEN_VERSION 由 tag 注入）
#
# Usage:
#   TAG(必填) 形如 v1.3.1; --yes 免交互; -m "备注" 可选发版说明
#     bash scripts/build-and-push.sh v1.3.1 --yes -m "本次新增 xxx"
set -euo pipefail

info() { echo -e "\033[32m>>> $*\033[0m"; }
warn() { echo -e "\033[33m!!! $*\033[0m"; }
error() { echo -e "\033[31mERROR: $*\033[0m"; exit 1; }

YES_MODE=0
TAG=""
MSG=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --yes) YES_MODE=1; shift ;;
        -m|--message)
            shift
            [ -n "${1:-}" ] || error "缺少 -m/--message 的备注内容"
            MSG="$1"
            shift
            ;;
        *) TAG="$1"; shift ;;
    esac
done

[[ -z "${TAG}" ]] && error "缺少TAG参数，示例: $0 v1.3.1 --yes"

cd "$(dirname "$0")/.."
TARGET_VER="${TAG#v}"
[[ "${TARGET_VER}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || error "TAG 格式错误，示例: v1.3.1"

# ===================== 重复Tag/Release自动清理 =====================
info "检查远端是否存在 Release ${TAG}"
if command -v gh >/dev/null 2>&1 && gh release view "${TAG}" >/dev/null 2>&1; then
    warn "发现已存在Release ${TAG}，准备删除Release并清理tag"
    gh release delete "${TAG}" -y --cleanup-tag
fi

info "清理本地&远端Git tag: ${TAG}"
git tag -d "${TAG}" 2>/dev/null || true
git push origin --delete "${TAG}" 2>/dev/null || true

# ===================== 版本号 bump =====================
info "执行版本号更新 ${TARGET_VER}"

SED_I=""
if sed --version 2>&1 | grep -q GNU; then SED_I=""; elif sed --version 2>&1 | grep -q busybox; then SED_I=""; else SED_I="''"; fi

BUMP_FILES=("internal/version/version.go" "package.json" "web/package.json" "Dockerfile" "Dockerfile.full")
for f in "${BUMP_FILES[@]}"; do
    [[ ! -f "${f}" ]] && error "缺失文件 ${f}"
done

if [[ "${SED_I}" == "''" ]]; then
    sed -i '' "s/^var Version = .*/var Version = \"${TARGET_VER}\"/" internal/version/version.go
    sed -i '' "s/^  \"version\": \".*\",\$/  \"version\": \"${TARGET_VER}\",/" package.json web/package.json
    sed -i '' "s/^ARG NOWEN_VERSION=.*/ARG NOWEN_VERSION=${TARGET_VER}/" Dockerfile Dockerfile.full
else
    sed -i "s/^var Version = .*/var Version = \"${TARGET_VER}\"/" internal/version/version.go
    sed -i "s/^  \"version\": \".*\",\$/  \"version\": \"${TARGET_VER}\",/" package.json web/package.json
    sed -i "s/^ARG NOWEN_VERSION=.*/ARG NOWEN_VERSION=${TARGET_VER}/" Dockerfile Dockerfile.full
fi

info "版本号确认:"
grep -n 'var Version' internal/version/version.go
grep -n '"version"' package.json web/package.json
grep -n '^ARG NOWEN_VERSION=' Dockerfile Dockerfile.full

# ===================== 写发版备注 =====================
info "写入发版备注 RELEASE_NOTES.md"
{
  printf '# %s\n\n' "${TAG}"
  if [ -n "${MSG}" ]; then
    printf '%s\n' "${MSG}"
  fi
} > RELEASE_NOTES.md

# ===================== Git 提交 & Tag =====================
info "提交版本变更"
git add RELEASE_NOTES.md internal/version/version.go package.json web/package.json Dockerfile Dockerfile.full
git commit -m "chore: bump version to ${TARGET_VER}" || info "无版本文件变更，跳过提交"
git push origin main

git tag "${TAG}"
git push origin "${TAG}"

# ===================== 交由 CI 自动构建发布 =====================
info "✅ 已推送 tag ${TAG}，GitHub Actions 将自动完成编译与 Release 创建"

info "查看发布结果: gh release view ${TAG}"
info "查看镜像: docker pull mobufan/fan-video:${TAG}"