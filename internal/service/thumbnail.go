package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const thumbnailFormat = "webp"

// thumbBaseDir 缩略图根目录（与数据卷分离，避免只读挂载问题）
const thumbBaseDir = "/cache/thumbs"

// GetThumbPath 生成缩略图文件路径，存放在 /cache/thumbs 下，保留原路径结构
func GetThumbPath(posterPath string) string {
	// 去掉前导 /，保留相对路径，替换扩展名为 .webp
	rel := strings.TrimPrefix(posterPath, "/")
	ext := filepath.Ext(rel)
	name := strings.TrimSuffix(rel, ext)
	return filepath.Join(thumbBaseDir, name+"."+thumbnailFormat)
}

// PurgeThumbnails 一键清空数据/清理缓存时删除全部海报缩略图（含其目录结构）。
// 缩略图属可再生缓存，直接删除整个 /cache/thumbs 目录树（父目录一起移除）；
// 目录不存在时返回 0 而非报错。
func PurgeThumbnails() (int, error) {
	if _, err := os.Stat(thumbBaseDir); os.IsNotExist(err) {
		return 0, nil
	}
	count := 0
	_ = filepath.Walk(thumbBaseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if filepath.Ext(path) == "."+thumbnailFormat {
			count++
		}
		return nil
	})
	if err := os.RemoveAll(thumbBaseDir); err != nil {
		return count, fmt.Errorf("清理海报缩略图目录失败: %w", err)
	}
	return count, nil
}
