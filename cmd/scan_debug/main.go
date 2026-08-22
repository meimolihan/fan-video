// scan_debug 只读诊断工具：打印扫描器对媒体库目录的识别决策，不写数据库。
//
// 用法:
//
//	go run ./cmd/scan_debug -path "/path/to/影视库" -type mixed
//
// type 可选: movie / tvshow / mixed（默认 mixed）
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/service"
	"go.uber.org/zap"
)

func main() {
	path := flag.String("path", "", "媒体库根目录路径")
	kind := flag.String("type", "mixed", "库类型: movie/tvshow/mixed")
	flag.Parse()

	if *path == "" {
		fmt.Println("用法: scan_debug -path /媒体库路径 [-type mixed]")
		fmt.Println("示例: go run ./cmd/scan_debug -path \"/mnt/media/影视库\" -type mixed")
		os.Exit(1)
	}

	cfg := &config.Config{}
	cfg.Cache.CacheDir = os.TempDir()
	scanner := service.NewScannerService(nil, nil, cfg, zap.NewNop().Sugar())

	for _, line := range scanner.DebugScanPlan(*path, *kind) {
		fmt.Println(line)
	}
}
