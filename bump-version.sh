#!/bin/bash
set -euo pipefail

gl_hui=$'\033[38;5;59m'
gl_huang=$'\033[38;5;11m'
gl_hong=$'\033[38;5;9m'
gl_lan=$'\033[38;5;32m'
gl_lv=$'\033[38;5;10m'
gl_qing=$'\033[38;5;14m'
gl_zi=$'\033[38;5;13m'
gl_bai=$'\033[38;5;15m'
gl_bufan=$'\033[38;5;14m'
reset=$'\033[0m'

# 自动适配 sed -i 参数
sed_i_arg() {
    if sed --version 2>&1 | grep -q GNU; then
        # GNU sed
        echo ""
    elif sed --version 2>&1 | grep -q busybox; then
        # busybox sed
        echo ""
    else
        # BSD/macOS sed 需要 ''
        echo "''"
    fi
}

usage() {
    echo -e "${gl_bai}用法: $0 <新版本号>${reset}"
    echo -e "${gl_qing}示例: $0 1.0.6${reset}"
}

main() {
    local NEW="${1:-}"
    if [[ -z "${NEW}" ]]; then
        echo -e "${gl_hong}错误：缺少新版本号参数${reset}"
        usage
        exit 1
    fi

    if ! [[ "${NEW}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo -e "${gl_hong}错误：版本号格式错误，需要语义化版本 如 1.0.6${reset}"
        exit 1
    fi

    local SCRIPT_DIR
    SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
    cd "${SCRIPT_DIR}"

    local files=(
        "package.json"
        "web/package.json"
        "Dockerfile"
        "Dockerfile.full"
    )
    for f in "${files[@]}"; do
        if [[ ! -f "${f}" ]]; then
            echo -e "${gl_hong}错误：缺失文件 ${f}，当前目录：${SCRIPT_DIR}${reset}"
            exit 1
        fi
    done

    local SED_I
    SED_I=$(sed_i_arg)
    if [[ "${SED_I}" == "''" ]]; then
        sed -i '' "s/^  \"version\": \".*\",\$/  \"version\": \"${NEW}\",/" package.json web/package.json
        sed -i '' "s/^ARG NOWEN_VERSION=.*/ARG NOWEN_VERSION=${NEW}/" Dockerfile Dockerfile.full
    else
        sed -i "s/^  \"version\": \".*\",\$/  \"version\": \"${NEW}\",/" package.json web/package.json
        sed -i "s/^ARG NOWEN_VERSION=.*/ARG NOWEN_VERSION=${NEW}/" Dockerfile Dockerfile.full
    fi

    echo -e "${gl_bufan}————————————————————————————————————————————————${reset}"
    echo -e "${gl_lv}✅ 修改结果确认（全部应显示 ${gl_huang}${NEW}${gl_lv}）${reset}"
    echo -e "${gl_bufan}————————————————————————————————————————————————${reset}"

    echo -e "${gl_zi}package.json / web/package.json version:${reset}"
    grep -n '"version"' package.json web/package.json || true

    echo ""
    echo -e "${gl_zi}Dockerfile / Dockerfile.full ARG NOWEN_VERSION:${reset}"
    grep -n "^ARG NOWEN_VERSION=" Dockerfile Dockerfile.full || true

    echo ""
    echo -e "${gl_lv}🎉 版本 bump 完成${reset}"
    echo -e "${gl_qing}下一步执行构建更新：${gl_bai}docker compose up -d --build${reset}"
}

main "$@"
