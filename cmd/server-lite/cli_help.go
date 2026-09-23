package main

import (
	"fmt"

	"github.com/fan-video/fan-video/internal/version"
)

// fvMainHelp 打印顶层帮助并退出。与 status/uninstall 帮助一致，但刻意不使用
// ANSI 颜色与方块制表符，避免在非 UTF-8 / 精简字体终端下显示乱码。
func fvMainHelp() {
	for _, line := range []string{
		"fan-video - 本地媒体服务器 (fan-video v" + version.Current() + ")",
		"",
		"用法:",
		"    fan-video [命令] [选项]      无命令时启动服务（前台运行）",
		"",
		"命令:",
		"    status                      查看服务运行状态（systemd / Docker / 进程）",
		"    uninstall                   卸载服务；-h 查看卸载选项",
		"    -v, --version               打印版本号后退出",
		"    -h, --help                  显示本帮助",
		"",
		"启动参数（通过环境变量覆盖）：",
		"    NOWEN_APP_PORT / SERVER_PORT   监听端口（默认 8080）",
		"",
		"示例:",
		"    fan-video                      启动服务",
		"    fan-video status               查看运行状态",
		"    NOWEN_APP_PORT=9000 fan-video  以 9000 端口启动",
	} {
		fmt.Println(line)
	}
}
