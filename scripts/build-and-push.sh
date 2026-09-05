#!/bin/bash
set -e

gl_hong='\033[0;31m'
gl_lv='\033[0;32m'
gl_huang='\033[1;33m'
gl_hui='\033[1;90m'
gl_end='\033[0m'

info(){ echo -e "${gl_lv}▶ $*${gl_end}"; }
warn(){ echo -e "${gl_huang}[警告] $*${gl_end}"; }
err(){ echo -e "${gl_hong}[错误] $*${gl_end}"; }

if [ $# -ne 1 ];then
    err "用法: $0 <版本号> 例 $0 1.2.3"
    exit 1
fi

VER="$1"
TAG="v${VER}"

info "版本: ${VER} tag: ${TAG}"

# 本地tag检测
if git rev-parse -q --verify "refs/tags/${TAG}" >/dev/null;then
    err "本地Git tag已存在: ${TAG}，请更换版本或删除本地tag"
    exit 1
fi

# 远端tag检测
if git ls-remote --tags origin | grep -q "refs/tags/${TAG}";then
    err "远端origin已存在tag ${TAG}，请更换版本号"
    exit 1
fi

info "执行make build"
make build VERSION="${VER}"

info "生成Release资产 fan-video_linux_amd64"
cp ./bin/fan-video ./bin/fan-video_linux_amd64

git add .
git commit -m "chore: release ${TAG}"
git push origin main

git tag "${TAG}"
git push origin "${TAG}"

# ---------- 创建/更新GitHub Release ----------
if command -v gh >/dev/null 2>&1; then
    info "检测到 gh cli，准备处理 GitHub Release ${TAG}"
    read -p "确认创建/更新Release ${TAG} ? [y/N] " ans
    if [[ "${ans}" =~ ^[yY]$ ]];then
        if gh release view "${TAG}" >/dev/null 2>&1; then
            info "Release ${TAG} 已存在，上传资产文件"
            gh release upload "${TAG}" ./bin/fan-video_linux_amd64 --clobber
        else
            info "新建 Release ${TAG}"
            gh release create "${TAG}" ./bin/fan-video_linux_amd64 \
                --title "Release ${TAG}" \
                --generate-notes
        fi
        info "✅ GitHub Release 处理完成"
    else
        warn "跳过Release，手动执行：gh release upload ${TAG} ./bin/fan-video_linux_amd64 --clobber"
    fi
else
    warn "未找到 gh cli：仅推送git tag，不会生成网页端GitHub Release"
    warn "安装：apt install gh && gh auth login"
fi

info "✅ 发布流程全部完成 ${TAG}"
