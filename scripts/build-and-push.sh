#!/bin/bash
set -euo pipefail

info() { echo -e "\033[32m>>> $*\033[0m"; }
warn() { echo -e "\033[33m!!! $*\033[0m"; }
error() { echo -e "\033[31mERROR: $*\033[0m"; exit 1; }

YES_MODE=0
TAG=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --yes) YES_MODE=1; shift ;;
        *) TAG="$1"; shift ;;
    esac
done

[[ -z "${TAG}" ]] && error "缺少TAG参数，示例: $0 v1.2.5 --yes"

cd "$(dirname "$0")/.."
TARGET_VER="${TAG#v}"

# ===================== 重复Tag/Release自动清理 =====================
info "检查远端是否存在 Release ${TAG}"
if gh release view "${TAG}" >/dev/null 2>&1; then
    warn "发现已存在Release ${TAG}，准备删除Release并清理tag"
    gh release delete "${TAG}" -y --cleanup-tag
fi

info "清理本地&远端Git tag: ${TAG}"
git tag -d "${TAG}" 2>/dev/null || true
git push origin --delete "${TAG}" 2>/dev/null || true

# ===================== 版本号 bump（内联） =====================
info "执行版本号更新 ${TARGET_VER}"

sed_i_arg() {
    if sed --version 2>&1 | grep -q GNU; then echo ""
    elif sed --version 2>&1 | grep -q busybox; then echo ""
    else echo "''"
    fi
}

SED_I=$(sed_i_arg)
BUMP_FILES=("package.json" "web/package.json" "Dockerfile" "Dockerfile.full")
for f in "${BUMP_FILES[@]}"; do
    [[ ! -f "${f}" ]] && error "缺失文件 ${f}"
done

if [[ "${SED_I}" == "''" ]]; then
    sed -i '' "s/^  \"version\": \".*\",\$/  \"version\": \"${TARGET_VER}\",/" package.json web/package.json
    sed -i '' "s/^ARG NOWEN_VERSION=.*/ARG NOWEN_VERSION=${TARGET_VER}/" Dockerfile Dockerfile.full
else
    sed -i "s/^  \"version\": \".*\",\$/  \"version\": \"${TARGET_VER}\",/" package.json web/package.json
    sed -i "s/^ARG NOWEN_VERSION=.*/ARG NOWEN_VERSION=${TARGET_VER}/" Dockerfile Dockerfile.full
fi

info "版本号确认:"
grep -n '"version"' package.json web/package.json
grep -n "^ARG NOWEN_VERSION=" Dockerfile Dockerfile.full

# ===================== 构建 =====================
info "执行 make build VERSION=${TARGET_VER}"
make build VERSION="${TARGET_VER}"

info "生成Release资产 fan-video_linux_amd64"
cp ./bin/fan-video ./bin/fan-video_linux_amd64

# ===================== Git 提交 & Tag =====================
info "提交版本变更"
git add package.json web/package.json Dockerfile Dockerfile.full
git commit -m "chore: bump version to ${TARGET_VER}" || info "无版本文件变更，跳过提交"
git push origin main

git tag "${TAG}"
git push origin "${TAG}"

# ===================== GitHub Release =====================
if command -v gh >/dev/null 2>&1; then
    info "检测到 gh cli，准备处理 GitHub Release ${TAG}"
    ans="n"
    if [[ ${YES_MODE} -eq 1 ]]; then
        ans="y"
    else
        read -p "确认创建Release ${TAG} ? [y/N] " ans
    fi

    if [[ "${ans}" =~ ^[yY]$ ]]; then
        info "新建 Release ${TAG}"
        gh release create "${TAG}" ./bin/fan-video_linux_amd64 \
            --title "Release ${TAG}" \
            --generate-notes
        info "✅ GitHub Release 处理完成"
    else
        warn "跳过Release创建"
    fi
else
    warn "未找到 gh cli：仅推送git tag，不会生成网页端GitHub Release"
    warn "安装：apt install gh && gh auth login"
fi

info "✅ 发布流程全部完成 ${TAG}"
