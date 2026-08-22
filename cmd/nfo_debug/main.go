// nfo_local_artwork_debug.go
// 诊断工具：测试本地海报匹配逻辑
// 使用方法：go run nfo_local_artwork_debug.go <视频文件路径>
//
// 示例：go run nfo_local_artwork_debug.go "/test/xxx.mp4"

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("用法: go run nfo_local_artwork_debug.go <视频文件路径>")
		fmt.Println("示例: go run nfo_local_artwork_debug.go \"/test/xxx.mp4\"")
		os.Exit(1)
	}

	mediaFilePath := os.Args[1]
	fmt.Printf("=== 本地海报匹配诊断 ===\n\n")
	fmt.Printf("输入路径: %s\n\n", mediaFilePath)

	// 模拟 FindLocalImagesForMedia 的逻辑
	dir := filepath.Dir(mediaFilePath)
	baseName := strings.TrimSuffix(filepath.Base(mediaFilePath), filepath.Ext(mediaFilePath))

	fmt.Printf("[阶段1] 视频所在目录: %s\n", dir)
	fmt.Printf("[阶段1] 基础文件名: %s\n\n", baseName)

	// 检查阶段1：同名图片
	fmt.Printf("[阶段1] 查找同名图片（视频目录）...\n")
	posterSuffixes := []string{
		"-poster.jpg", "-poster.png", "-poster.webp",
		"-cover.jpg", "-cover.png", "-cover.webp",
		"-thumb.jpg", "-thumb.png", "-thumb.webp",
		".jpg", ".png", ".webp",
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Printf("[错误] 无法读取目录 %s: %v\n", dir, err)
		os.Exit(1)
	}

	fmt.Printf("[调试] 目录内容:\n")
	for _, e := range entries {
		fmt.Printf("  - %s (%s)\n", e.Name(), map[bool]string{true: "目录", false: "文件"}[e.IsDir()])
	}
	fmt.Println()

	for _, suffix := range posterSuffixes {
		path := filepath.Join(dir, baseName+suffix)
		if _, err := os.Stat(path); err == nil {
			fmt.Printf("[阶段1] ✅ 找到: %s\n", path)
			return
		}
	}
	fmt.Printf("[阶段1] ❌ 未找到同名图片\n\n")

	// 阶段1b：子目录中的同名图片
	fmt.Printf("[阶段1b] 查找子目录中的同名图片...\n")
	subDirs := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			subDirs = append(subDirs, entry.Name())
		}
	}

	if len(subDirs) == 0 {
		fmt.Printf("[阶段1b] ❌ 无子目录\n")
	} else {
		fmt.Printf("[阶段1b] 子目录列表: %v\n\n", subDirs)

		for _, sub := range subDirs {
			subDir := filepath.Join(dir, sub)
			fmt.Printf("[阶段1b] 检查子目录: %s\n", subDir)

			for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
				path := filepath.Join(subDir, baseName+ext)
				if _, err := os.Stat(path); err == nil {
					fmt.Printf("[阶段1b] ✅ 找到: %s\n", path)
					return
				} else {
					fmt.Printf("[阶段1b]   - %s 不存在\n", path)
				}
			}
		}
	}

	fmt.Printf("\n[结论] 未找到海报图片\n")

	// 提供修复建议
	fmt.Printf("\n=== 修复建议 ===\n")
	fmt.Printf("请确保海报文件存在且命名正确：\n")
	fmt.Printf("  选项1: %s/%s.jpg (直接放在视频目录)\n", dir, baseName)
	fmt.Printf("  选项2: %s/任意子目录/%s.jpg (放在子目录中)\n", dir, baseName)
	fmt.Printf("\n支持的图片格式: .jpg .jpeg .png .webp\n")
}
