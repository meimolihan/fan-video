package main

import (
	"fmt"
)

// 简易 CLI 输出工具，风格与 scripts/install.sh / scripts/uninstall.sh 的色板对齐。

const (
	cliGrey   = "\033[38;5;59m"
	cliRed    = "\033[38;5;9m"
	cliGreen  = "\033[38;5;10m"
	cliYellow = "\033[38;5;11m"
	cliBlue   = "\033[38;5;32m"
	cliWhite  = "\033[38;5;15m"
	cliPurple = "\033[38;5;13m"
	cliCyan   = "\033[38;5;14m"
	cliReset  = "\033[0m"
)

func cliPaintColor(s, color string) string {
	return color + s + cliReset
}

func cliErr(f string, a ...interface{}) {
	fmt.Printf("  %s %s\n", cliPaintColor("[错误]", cliRed), fmt.Sprintf(f, a...))
}

func cliWarn(f string, a ...interface{}) {
	fmt.Printf("  %s %s\n", cliPaintColor("[警告]", cliYellow), fmt.Sprintf(f, a...))
}

func cliHext(f string, a ...interface{}) {
	fmt.Printf("  %s %s\n", cliPaintColor("[提示]", cliCyan), fmt.Sprintf(f, a...))
}

func cliOK(f string, a ...interface{}) {
	fmt.Printf("  %s %s\n", cliPaintColor(">>>", cliGreen), fmt.Sprintf(f, a...))
}

func cliDone(f string, a ...interface{}) {
	fmt.Printf("  %s %s\n", cliPaintColor("✔", cliGreen), fmt.Sprintf(f, a...))
}

func cliSep() {
	fmt.Println(cliPaintColor("———————————————————", cliCyan))
}

func cliSection(title string) {
	fmt.Printf("  %s %s\n", cliPaintColor("▶", cliPurple), title)
}

func cliKV(k, v string) {
	fmt.Printf("  %-14s %s\n", cliPaintColor(k, cliBlue), cliPaintColor(v, cliWhite))
}

func cliBanner(title string) {
	fmt.Println(cliPaintColor(`
   ███████╗ █████╗ ███╗   ██╗     ██╗   ██╗██╗██████╗ ███████╗ ██████╗
   ██╔════╝██╔══██╗████╗  ██║     ██║   ██║██║██╔══██╗██╔════╝██╔═══██╗
   █████╗  ███████║██╔██╗ ██║     ██║   ██║██║██║  ██║█████╗  ██║   ██║
   ██╔══╝  ██╔══██║██║╚██╗██║     ╚██╗ ██╔╝██║██║  ██║██╔══╝  ██║   ██║
   ██║     ██║  ██║██║ ╚████║      ╚████╔╝ ██║██████╔╝███████╗╚██████╔╝
   ╚═╝     ╚═╝  ╚═╝╚═╝  ╚═══╝       ╚═══╝  ╚═╝╚═════╝ ╚══════╝ ╚═════╝`, cliPurple))
	fmt.Printf("%s — %s\n\n", cliPaintColor("fan-video", cliWhite), cliPaintColor(title, cliCyan))
}

// cliConfirm 询问 y/n；defYes 为 true 时回车默认「是」。
func cliConfirm(q string, defYes bool) bool {
	sc := "n"
	if defYes {
		sc = "y"
	}
	fmt.Printf("  %s %s/%s: ", cliPaintColor(q, cliWhite), cliPaintColor("y", cliGreen), cliPaintColor("n", cliRed))
	var ans string
	if _, err := fmt.Scanln(&ans); err != nil {
		return sc == "y"
	}
	switch ans {
	case "y", "Y", "yes", "YES", "":
		return sc == "y"
	}
	return false
}

func cliHumanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
