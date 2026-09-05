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

# 默认值
BACKUP_DIR="/vol2/1000/file/backup/fan-video-backup"
KEEP_NUM=6
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

# 参数解析，支持两种传参顺序可互换
# 用法示例：
# ./fan-video_backup.sh                     # 使用默认
# ./fan-video_backup.sh 10                  # 只传保留份数
# ./fan-video_backup.sh /data/bak           # 只传备份目录
# ./fan-video_backup.sh /data/bak 8         # 目录 保留数
# ./fan-video_backup.sh 8 /data/bak         # 保留数 目录，顺序互换兼容
parse_args() {
    local p1="${1:-}"
    local p2="${2:-}"

    if [[ -z "${p1}" ]]; then
        return
    fi

    if [[ "${p1}" =~ ^[0-9]+$ ]]; then
        KEEP_NUM="${p1}"
        if [[ -n "${p2}" ]]; then
            BACKUP_DIR="${p2}"
        fi
    else
        BACKUP_DIR="${p1}"
        if [[ -n "${p2}" && "${p2}" =~ ^[0-9]+$ ]]; then
            KEEP_NUM="${p2}"
        fi
    fi
}
parse_args "$@"

echo -e "${gl_zi}>>> 备份 fan-video${gl_bai}"
echo -e "${gl_bufan}————————————————————————————————————————————————${gl_bai}"
echo -e "${gl_huang}保存目录：${gl_lv}${BACKUP_DIR}${gl_bai}"
echo -e "${gl_huang}保留数量：${gl_lv}${KEEP_NUM}${gl_bai}"
echo -e "${gl_huang}数据目录：${gl_lv}${DATA_DIR}${gl_bai}"

command -v systemctl >/dev/null 2>&1 || { echo -e "${gl_hong}❌ 未检测到 systemctl，无法备份 services${gl_bai}"; exit 1; }

[ -d "${DATA_DIR}" ] || { echo -e "${gl_hong}❌ 数据目录不存在: ${DATA_DIR}${gl_bai}"; exit 1; }

# 文件名精确到秒 YYYY-MM-DD_HH-MM-SS
NOW=$(date +%Y-%m-%d_%H-%M-%S)
BACKUP_FILE="${BACKUP_DIR}/FanVideo-${NOW}.tar.gz"

mkdir -p "${BACKUP_DIR}"

echo -e ""
echo -e "${gl_huang}>>> 停止 ${SERVICE} 服务${gl_bai}"
systemctl stop ${SERVICE}

echo -e ""
echo -e "${gl_lan}>>> 执行备份：${BACKUP_FILE}${gl_bai}"
if tar -czf "${BACKUP_FILE}" -C "${DATA_DIR}" .; then
    echo -e "${gl_lv}>>> 备份完成：${BACKUP_FILE}${gl_bai}"
else
    echo -e "${gl_hong}❌ 备份失败，正在恢复服务${gl_bai}"
    systemctl start ${SERVICE}
    exit 1
fi

cd "${BACKUP_DIR}" || exit 1

# 正则适配新格式：FanVideo-YYYY-MM-DD_HH-MM-SS.tar.gz
old_files=$(find . -maxdepth 1 -type f -name "FanVideo-*.tar.gz" -printf "%f\n" \
| sed -E 's/^FanVideo-([0-9]{4}-[0-9]{2}-[0-9]{2}_[0-9]{2}-[0-9]{2}-[0-9]{2})\.tar\.gz$/\1 &/' \
| sort -k1,1 \
| head -n -${KEEP_NUM} \
| awk '{print $2}')

if [ -n "${old_files}" ];then
    echo -e ""
    echo -e "${gl_hong}>>> 将删除过期备份文件：${gl_bai}"
    echo "${old_files}"
    echo "${old_files}" | xargs -I {} rm -f "./{}"
else
    echo -e "${gl_lv}>>> 无过期备份需要删除${gl_bai}"
fi

echo -e ""
echo -e "${gl_huang}>>> 启动 ${SERVICE} 服务${gl_bai}"
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