//go:build windows

package service

import (
	"os"
)

// getFileOwner 获取文件所有者（Windows 平台简化实现）
func getFileOwner(info os.FileInfo) string {
	return "-"
}
