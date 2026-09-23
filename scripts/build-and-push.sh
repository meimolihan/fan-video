#!/bin/bash
#
# fan-video (Nowen Video) - 发布脚本（触发 GitHub Actions 自动构建）
# 不在本地编译任何产物：仅更新版本号、推送代码并打 v 开头 tag。
# 推送代码、打 tag 后显式调用 gh workflow run 触发发布流水线（日常 push 不会触发）：
#   release.yml -> amd64/arm64 自包含二进制（内嵌前端 dist/PWA）创建 GitHub Release
#                  + multi-arch Docker 镜像（latest + 版本标签，NOWEN_VERSION 由 tag 注入）
#
# Usage:
#   TAG(必填) 形如 v1.3.1; --yes 免交互; -m "备注" 可选发版说明
#     bash scripts/build-and-push.sh v1.3.1 --yes -m "本次新增 xxx"
set -euo pipefail

info() { echo -e "${gl_lv}>>> $*${reset}"; }
warn() { echo -e "${gl_huang}!!! $*${reset}"; }
error() { echo -e "${gl_hong}ERROR: $*${reset}"; exit 1; }

list_color_init() {
    export gl_hui=$'\033[38;5;59m'
    export gl_hong=$'\033[38;5;9m'
    export gl_lv=$'\033[38;5;10m'
    export gl_huang=$'\033[38;5;11m'
    export gl_lan=$'\033[38;5;32m'
    export gl_bai=$'\033[38;5;15m'
    export gl_zi=$'\033[38;5;13m'
    export gl_bufan=$'\033[38;5;14m'
    export reset=$'\033[0m'
}
list_color_init


# 拉取本次 tag 触发的 workflow run：tag push 后 Actions 尚未注册新 run，
# 直接 --limit 1 取最新会取到上一次的陈旧记录。
# 这里按 tag 过滤并轮询等待，且用 headSha 校验确实是本次 push 的 run。
get_gh_run_info() {
    local tag="$1"
    local expect_sha
    expect_sha=$(git rev-parse HEAD 2>/dev/null) || expect_sha=""

    local tries=0
    local max_tries=12
    local run_json=""
    local sha=""
    while (( tries < max_tries )); do
        run_json=$(gh run list --workflow=release.yml --limit 1 --branch "${tag}" \
            --json status,displayTitle,headBranch,event,databaseId,startedAt,headSha 2>/dev/null) || run_json=""
        if [[ -n "$run_json" && "$run_json" != "[]" ]]; then
            sha=$(echo "$run_json" | jq -r '.[0].headSha')
            if [[ -z "$expect_sha" || "$sha" == "$expect_sha" ]]; then
                echo "$run_json"
                return 0
            fi
        fi
        tries=$((tries + 1))
        if (( tries < max_tries )); then
            sleep 5
        fi
    done
    return 1
}

calc_elapsed() {
    local start_iso="$1"
    local start_ts
    start_ts=$(date -d "${start_iso}" +%s 2>/dev/null)
    if [[ -z "$start_ts" ]]; then
        echo "时间解析失败"
        return
    fi
    local now_ts=$(date +%s)
    local diff=$(( now_ts - start_ts ))

    if (( diff < 60 )); then
        echo "${diff} 秒"
    elif (( diff < 3600 )); then
        echo "$((diff / 60)) 分 $((diff % 60)) 秒"
    else
        echo "$((diff / 3600)) 时 $(((diff % 3600)/60)) 分"
    fi
}

beautify_gh_run() {
    local tag="${1:-}"
    echo -e ""
    echo -e "${gl_zi}>>> GitHub Actions Release 流水线信息${gl_bai}"
    echo -e "${gl_bufan}————————————————————————————————————————————————${gl_bai}"

    json_data=$(get_gh_run_info "${tag}")
    if [[ -z "$json_data" || "$json_data" == "[]" ]]; then
        echo -e "${gl_hong}[错误] 未获取到本次(${tag})流水线运行记录${reset}"
        echo -e "${gl_bai}可能原因：GitHub Actions 尚未注册该 run，可稍后用以下命令手动查看：${reset}"
        echo -e "${gl_lv}gh run list --workflow=release.yml --branch ${tag}${reset}"
        echo -e "${gl_bufan}————————————————————————————————————————————————${gl_bai}"
        exit 1
    fi

    status=$(echo "$json_data" | jq -r '.[0].status')
    title=$(echo "$json_data" | jq -r '.[0].displayTitle')
    branch=$(echo "$json_data" | jq -r '.[0].headBranch')
    event=$(echo "$json_data" | jq -r '.[0].event')
    run_id=$(echo "$json_data" | jq -r '.[0].databaseId')
    started_at=$(echo "$json_data" | jq -r '.[0].startedAt')

    elapsed=$(calc_elapsed "$started_at")

    case "$status" in
        in_progress)
            status_text="${gl_huang}运行中${reset}"
            ;;
        completed)
            conclusion=$(gh run view "$run_id" --json conclusion | jq -r '.conclusion')
            case "$conclusion" in
                success) status_text="${gl_lv}成功${reset}";;
                failure) status_text="${gl_hong}失败${reset}";;
                cancelled) status_text="${gl_hui}已取消${reset}";;
                skipped) status_text="${gl_huang}已跳过${reset}";;
                *) status_text="${gl_huang}已完成(${conclusion})${reset}";;
            esac
            ;;
        *)
            status_text="${gl_hui}${status}${reset}"
            ;;
    esac

    printf "%-14s%s\n" "${gl_hui}[运行状态]：${reset}" "$status_text"
    printf "%-14s%s\n" "${gl_hui}[提交标题]：${reset}" "${gl_huang}$title${reset}"
    printf "%-14s%s\n" "${gl_hui}[触发分支]：${reset}" "${gl_lan}$branch${reset}"
    printf "%-14s%s\n" "${gl_hui}[触发事件]：${reset}" "${gl_bai}$event${reset}"
    printf "%-14s%s\n" "${gl_hui}[Run ID]：${reset}" "${gl_bufan}$run_id${reset}"
    printf "%-14s%s\n" "${gl_hui}[已耗时]：${reset}" "${gl_bai}$elapsed${reset}"

    echo -e "${gl_bufan}————————————————————————————————————————————————${gl_bai}"
    echo -e ""
    echo -e "${gl_huang}>>> 快捷操作命令${gl_bai}"
    echo -e "${gl_bufan}————————————————————————————————————————————————${gl_bai}"
    echo -e "${gl_lv}实时跟踪流水线：${reset}gh run watch $run_id"
    echo -e "${gl_lv}查看详细信息：${reset}gh run view $run_id"
    echo -e "${gl_lv}查看完整日志：${reset}gh run view $run_id --log"
    echo -e "${gl_lv}查看失败日志：${reset}gh run view $run_id --log-failed"
    echo -e "${gl_lv}取消本次构建：${reset}gh run cancel $run_id"
    echo -e "${gl_lv}重新运行流水线：${reset}gh run rerun $run_id"
    echo -e "${gl_bufan}————————————————————————————————————————————————${gl_bai}"
}

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

[[ "${TAG}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || error "TAG 格式应为 vX.Y.Z，例如 v1.3.1（当前: ${TAG}）"

cd "$(dirname "$0")/.."

# ===================== CNB 同名仓库检测与自动创建 =====================
ensure_cnb_repo() {
    local token="${CNB_ACCESS_TOKEN:-}"
    if [ -z "${token}" ] && [ -f "${HOME}/.cnb_token" ]; then
        token=$(<"${HOME}/.cnb_token")
    fi
    if [ -z "${token}" ]; then
        warn "未配置 CNB_ACCESS_TOKEN 或 ~/.cnb_token，跳过 CNB 仓库检测"
        return 0
    fi
    local origin repo cname code
    origin=$(git config --get remote.origin.url 2>/dev/null || true)
    repo=$(basename "${origin}" .git 2>/dev/null || true)
    [ -z "${repo}" ] && repo=$(basename "$(pwd)" 2>/dev/null || true)
    cname="cnb.cool/meimolihan/${repo}"
    if GIT_TERMINAL_PROMPT=0 git ls-remote "https://cnb:${token}@${cname}.git" >/dev/null 2>&1; then
        info "CNB 同名仓库已存在：${cname}，跳过创建"
        return 0
    fi
    info "CNB 仓库不存在：${cname}，正在自动创建 ..."
    code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "https://api.cnb.cool/meimolihan/-/repos" \
        -H "Authorization: Bearer ${token}" \
        -H "Accept: application/vnd.cnb.api+json" \
        -H "Content-Type: application/json" \
        -d "{\"name\":\"${repo}\",\"visibility\":\"public\"}")
    if [ "${code}" = "201" ] || [ "${code}" = "200" ]; then
        info "CNB 仓库创建成功：${cname}（HTTP ${code}）"
    else
        warn "CNB 仓库创建返回 HTTP ${code}（可能已存在或权限不足），继续发布"
    fi
}
ensure_cnb_repo
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
  if [ -n "${MSG}" ]; then
    printf '%s\n' "${MSG}"
    printf '\n'
  fi
  printf '```bash\n'
  printf 'docker pull mobufan/fan-video:latest\n'
  printf '```\n'
  printf '```bash\n'
  printf 'docker pull mobufan/fan-video:%s\n' "${TAG}"
  printf '```\n'
  printf '\n'
  printf '```bash\n'
  printf 'docker pull ghcr.io/meimolihan/fan-video:latest\n'
  printf '```\n'
  printf '```bash\n'
  printf 'docker pull ghcr.io/meimolihan/fan-video:%s\n' "${TAG}"
  printf '```\n'
  printf '\n'
  printf '## 二进制安装\n'
  printf '```bash\n'
  printf 'bash -c "$(curl -sSL https://raw.githubusercontent.com/meimolihan/fan-video/main/scripts/install.sh)" -p 9060 -d /var/lib/fan-video\n'
  printf '```\n'
  printf '\n'
  printf '## 二进制卸载\n'
  printf '```bash\n'
  printf 'bash -c "$(curl -sSL https://raw.githubusercontent.com/meimolihan/fan-video/main/scripts/uninstall.sh)" -y --purge\n'
  printf '```\n'
} > RELEASE_NOTES.md

# ===================== Git 提交 & Tag =====================
info "提交版本变更"
git add .
git commit -m "chore: bump version to ${TARGET_VER}" || info "无版本文件变更，跳过提交"
git push origin main

git tag "${TAG}"
git push origin "${TAG}"

# ===================== 触发 GitHub Actions 发布流水线 =====================
# 发布流水线仅支持 workflow_dispatch 手动触发（日常 push / 打 tag 不会触发）。
# 推送完 tag 后，显式调用 gh workflow run 启动发布流水线。
info "触发 GitHub Actions 发布流水线 (release.yml, tag=${TAG})"
if ! command -v gh >/dev/null 2>&1; then
    warn "未安装 gh CLI，无法自动触发流水线。"
    warn "请手动运行: gh workflow run release.yml -f tag=${TAG} --ref ${TAG}"
    exit 0
fi
if ! gh workflow run release.yml -f tag="${TAG}" --ref "${TAG}" >/dev/null 2>&1; then
    warn "gh workflow run 触发失败，请检查："
    warn "  1. 已执行过 gh auth login 且 token 含 workflow 权限"
    warn "  2. 远端已推送 tag ${TAG}"
    warn "  可手动触发: gh workflow run release.yml -f tag=${TAG} --ref ${TAG}"
    exit 1
fi
info "✅ 已推送 tag ${TAG} 并触发 GitHub Actions 发布流水线"

info "查看发布结果: gh release view ${TAG}"
info "查看镜像: docker pull mobufan/fan-video:${TAG}"

echo -e "${gl_bai}远程安装命令： ${gl_lv}bash -c \"\$(curl -sSL https://raw.githubusercontent.com/meimolihan/fan-video/main/scripts/install.sh)\" -p 9060 -d /var/lib/fan-video${gl_bai}"

beautify_gh_run "${TAG}"