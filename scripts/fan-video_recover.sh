#!/bin/bash
set -uo pipefail

gl_hui='\033[38;5;59m'
gl_hong='\033[38;5;9m'
gl_lv='\033[38;5;10m'
gl_huang='\033[38;5;11m'
gl_lan='\033[38;5;32m'
gl_bai='\033[38;5;15m'
gl_zi='\033[38;5;13m'
gl_bufan='\033[38;5;14m'

# 默认备份目录
BACKUP_DIR="/vol2/1000/file/backup/fan-video-backup"
SERVICE="fan-video"
CONFIG_FILE="/etc/fan-video.conf"
DATA_DIR="/var/lib/fan-video"

# 从安装记录读取数据目录（不存在时使用默认值）
if [ -f "${CONFIG_FILE}" ]; then
  while IFS='=' read -r KEY VALUE; do
    KEY=$(printf '%s' "$KEY" | tr -d ' ')
    [ "$KEY" = "DATA_DIR" ] && [ -n "$VALUE" ] && DATA_DIR="${VALUE}"
  done < "${CONFIG_FILE}"
fi

# 参数解析：支持传备份目录，兼容逻辑同备份脚本
# 用法：
# ./fan-video_recover.sh                     # 使用默认目录，取最新备份
# ./fan-video_recover.sh /data/bak           # 指定备份目录，取该目录最新备份
parse_args() {
    local p1="${1:-}"
    if [[ -n "${p1}" ]]; then
        BACKUP_DIR="${p1}"
    fi
}
parse_args "$@"

echo -e "${gl_zi}>>> fan-video 恢复脚本${gl_bai}"
echo -e "${gl_bufan}————————————————————————————————————————————————${gl_bai}"
echo -e "${gl_huang}备份目录：${gl_lv}${BACKUP_DIR}${gl_bai}"
echo -e "${gl_huang}数据目录：${gl_lv}${DATA_DIR}${gl_bai}"

command -v systemctl >/dev/null 2>&1 || { echo -e "${gl_hong}❌ 未检测到 systemctl，无法恢复服务${gl_bai}"; exit 1; }

mkdir -p "${BACKUP_DIR}"

echo -e ""
echo -e "${gl_lan}>>> 查找最新备份文件${gl_bai}"
f=$(find "$BACKUP_DIR" -maxdepth 1 -type f -name "FanVideo-*.tar.gz" -printf "%f\n" \
| sed -E 's/^FanVideo-([0-9]{4}-[0-9]{2}-[0-9]{2}(_[0-9]{2}-[0-9]{2}-[0-9]{2})?)\.tar\.gz$/\1 &/' \
| sort -k1,1 \
| tail -n1 \
| awk '{print $2}')

if [ -z "$f" ];then
    echo -e "${gl_hong}❌ 无备份文件，退出${gl_bai}"
    exit 1
fi

RESTORE_FILE="${BACKUP_DIR}/${f}"
echo -e "${gl_huang}恢复文件: ${gl_lv}${RESTORE_FILE}${gl_bai}"

echo -e ""
echo -e "${gl_huang}>>> 停止 ${SERVICE} 服务${gl_bai}"
systemctl stop ${SERVICE}

echo -e ""
echo -e "${gl_huang}>>> 执行恢复${gl_bai}"
mkdir -p "${DATA_DIR}"
if tar -xzf "${RESTORE_FILE}" -C "${DATA_DIR}"; then
    echo -e "${gl_lv}>>> 恢复完成${gl_bai}"
else
    echo -e "${gl_hong}❌ 恢复失败，正在启动服务${gl_bai}"
    systemctl start ${SERVICE}
    exit 1
fi

echo -e ""
echo -e "${gl_huang}>>> 重载systemd配置并启动服务${gl_bai}"
systemctl daemon-reload
systemctl start ${SERVICE}

sleep 2
STATUS=$(systemctl is-active ${SERVICE})
case "${STATUS}" in
    active)
        echo -e "${gl_lv}✅ 服务状态：运行中${gl_bai}"
        ;;
    inactive)
        echo -e "${gl_hong}❌ 服务状态：已停止${gl_bai}"
        ;;
    failed)
        echo -e "${gl_hong}❌ 服务状态：启动失败${gl_bai}"
        ;;
    *)
        echo -e "${gl_huang}⚠️ 服务状态：${STATUS}${gl_bai}"
        ;;
esac
echo -e "${gl_bufan}————————————————————————————————————————————————${gl_bai}"