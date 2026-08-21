//go:build !windows

package service

import (
	"fmt"
	"os"
	"os/user"
	"syscall"
)

// getFileOwner 获取文件所有者（Unix/Linux/Mac）
func getFileOwner(info os.FileInfo) string {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "-"
	}
	uid := fmt.Sprintf("%d", stat.Uid)
	u, err := user.LookupId(uid)
	if err != nil {
		return uid
	}
	return u.Username
}
