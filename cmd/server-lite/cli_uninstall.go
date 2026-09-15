package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// fvUninstall 卸载 fan-video：停止并移除 systemd 服务、移除容器、关闭防火墙端口、
// 删除二进制与安装记录，可选删除数据目录。
func fvUninstall(args []string) int {
	yes, purge, keep := false, false, false
	for _, a := range args {
		switch a {
		case "-y", "--yes":
			yes = true
		case "--purge", "--delete-data":
			purge = true
		case "--keep-data":
			keep = true
		case "-h", "--help":
			fvUninstallUsage()
			return 0
		default:
			cliErr("未知参数: %s，使用 -h 查看帮助", a)
			return 1
		}
	}
	if os.Geteuid() != 0 {
		cliErr("请以 root 身份运行：sudo fan-video uninstall")
		return 1
	}
	if purge && keep {
		cliErr("--purge 与 --keep-data 不能同时使用")
		return 1
	}

	cliBanner("卸载")
	cliSep()

	if !yes && !cliConfirm("卸载将停止并移除 fan-video 服务与程序，是否继续", false) {
		fmt.Println(cliPaintColor("已取消卸载。", cliYellow))
		return 0
	}

	if port, ok := fvReadRecord("PORT"); ok {
		if p, err := strconv.Atoi(port); err == nil && p > 0 && p <= 65535 {
			fvMSCloseFirewallPort(p)
		}
	} else {
		fvMSCloseFirewallPort(fvDefaultPort)
	}

	if _, err := os.Stat(fvServiceFile); err == nil {
		cliDone("正在停止并移除 systemd 服务 fan-video ...")
		fvMSRun("systemctl", "stop", fvServiceName)
		fvMSRun("systemctl", "disable", fvServiceName)
		_ = os.Remove(fvServiceFile)
		fvMSRun("systemctl", "daemon-reload")
		fvMSRun("systemctl", "reset-failed")
	}

	if cmd, err := exec.LookPath("docker"); err == nil {
		if out, e := exec.Command(cmd, "ps", "-a", "--filter", "name="+fvBinName, "--format", "{{.Names}}").Output(); e == nil {
			for _, n := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				n = strings.TrimSpace(n)
				if n == fvBinName {
					cliDone("正在移除容器 %s ...", n)
					fvMSRun(cmd, "rm", "-f", n)
				}
			}
		}
	}

	pids := make([]int, 0)
	for _, p := range fvRunningProcs() {
		pids = append(pids, p.pid)
	}
	if len(pids) > 0 {
		cliDone("正在停止 fan-video 进程: %v ...", pids)
		for _, pid := range pids {
			if pr, err := os.FindProcess(pid); err == nil {
				_ = pr.Signal(syscall.SIGTERM)
			}
		}
		time.Sleep(time.Second)
		for _, pid := range pids {
			if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid))); err == nil {
				if pr, err := os.FindProcess(pid); err == nil {
					_ = pr.Signal(syscall.SIGKILL)
				}
			}
		}
	}

	fvMSRemoveBinary()

	dataDir := fvDetectDataDir(fvDefaultData)
	info, statErr := os.Stat(dataDir)
	if statErr != nil || !info.IsDir() {
		cliDone("未检测到数据目录 %s，跳过删除。", dataDir)
	} else {
		if size, err := fvMSDirSize(dataDir); err == nil {
			cliDone("检测到数据目录: %s（约 %s）", dataDir, cliHumanSize(size))
		}
		remove := false
		switch {
		case purge:
			remove = true
		case keep:
			remove = false
		case yes:
			remove = false
		default:
			remove = cliConfirm(fmt.Sprintf("是否删除数据目录 %s（完全卸载）", dataDir), true)
		}
		if remove {
			if err := os.RemoveAll(dataDir); err != nil {
				cliWarn("删除数据目录失败: %v", err)
			} else {
				cliDone("已删除数据目录 %s", dataDir)
			}
		} else {
			cliDone("已保留数据目录 %s", dataDir)
		}
	}

	_ = os.Remove(fvRecordFile)
	cliDone("已删除安装记录 %s", fvRecordFile)

	cliDone("fan-video 卸载完成")
	fmt.Println(cliPaintColor("如需重新安装，请重新部署（install.sh / docker compose）。", cliGrey))
	return 0
}

func fvUninstallUsage() {
	for _, line := range []string{
		"用法: fan-video uninstall [选项]",
		"",
		"选项:",
		"    -y, --yes        免确认，静默卸载（默认保留数据目录）",
		"    --purge          卸载时同时删除数据目录",
		"    --keep-data      卸载时保留数据目录",
		"    -h, --help       显示帮助",
		"",
		"示例:",
		"    fan-video uninstall -y          免确认卸载，保留数据目录",
		"    fan-video uninstall -y --purge  免确认卸载，并删除数据目录",
	} {
		fmt.Println(cliPaintColor(line, cliWhite))
	}
}

func fvMSRemoveBinary() {
	paths := map[string]string{"/usr/local/bin/fan-video": "/usr/local/bin/fan-video"}
	if exe, err := os.Executable(); err == nil {
		paths[exe] = exe
	}
	for _, p := range paths {
		if _, err := os.Lstat(p); err != nil {
			continue
		}
		if err := os.Remove(p); err != nil {
			cliWarn("删除二进制文件 %s 失败: %v", p, err)
		} else {
			cliDone("已删除二进制文件 %s", p)
		}
	}
}

func fvMSCloseFirewallPort(port int) {
	portStr := strconv.Itoa(port)
	if cmd, err := exec.LookPath("firewall-cmd"); err == nil {
		if out, _ := exec.Command(cmd, "--state").Output(); strings.TrimSpace(string(out)) == "running" {
			fvMSRun(cmd, "--permanent", "--remove-port="+portStr+"/tcp")
			fvMSRun(cmd, "--reload")
			cliDone("已通过 firewalld 关闭端口 %s/tcp", portStr)
			return
		}
	}
	if cmd, err := exec.LookPath("ufw"); err == nil {
		if out, _ := exec.Command(cmd, "status").Output(); strings.Contains(string(out), "active") {
			fvMSRun(cmd, "delete", "allow", portStr+"/tcp")
			cliDone("已通过 ufw 关闭端口 %s/tcp", portStr)
			return
		}
	}
	if cmd, err := exec.LookPath("iptables"); err == nil {
		if err := exec.Command(cmd, "-D", "INPUT", "-p", "tcp", "--dport", portStr, "-j", "ACCEPT").Run(); err == nil {
			cliDone("已通过 iptables 关闭端口 %s/tcp", portStr)
		}
	}
}

func fvMSRun(name string, args ...string) {
	if _, err := exec.LookPath(name); err != nil {
		return
	}
	_ = exec.Command(name, args...).Run()
}

func fvMSDirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}
