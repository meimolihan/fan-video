package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// fan-video 安装/运行约定（与 scripts/install.sh、scripts/uninstall.sh 保持一致）。
const (
	fvBinName     = "fan-video"
	fvServiceName = "fan-video"
	fvServiceFile = "/etc/systemd/system/fan-video.service"
	fvRecordFile  = "/etc/fan-video.conf"
	fvDefaultData = "/var/lib/fan-video"
	fvDefaultPort = 8080
)

type fvProc struct {
	pid  int
	data string
}

var (
	fvDataArgRe = regexp.MustCompile(`(-d|--data)\s+(?:([^"' \t]+)|"([^"]+)"|'([^']+)')`)
)

// fvStatus 显示服务状态：systemd > Docker > /proc 进程。
func fvStatus() int {
	cliBanner("服务状态")
	cliSep()

	if port, ok := fvReadRecord("PORT"); ok {
		cliKV("install 记录端口", port)
	}
	if dir, ok := fvReadRecord("DATA_DIR"); ok {
		cliKV("install 记录数据目录", dir)
	}

	typ, pid := fvFindProcess("")
	if typ == "" || pid == 0 {
		cliErr("%s 服务未运行", fvBinName)
		cliSep()
		return 1
	}

	cliSection("服务方式")
	cliKV("运行方式", typ)
	cliSection("进程信息")
	cliKV("进程 PID", strconv.Itoa(pid))
	cliKV("线程数量", fvProcField(pid, "Threads"))
	cliSection("网络")
	if port := fvListenPort(pid); port != "" {
		cliKV("监听端口", port)
	} else {
		cliKV("监听端口", "未找到")
	}
	cliSection("运行时间")
	cliKV("已运行", fvFormatUptime(fvProcUptimeSec(pid)))
	cliSection("内存")
	cliKV("虚拟内存", fvFormatMem(fvProcField(pid, "VmSize")))
	cliKV("物理内存", fvFormatMem(fvProcField(pid, "VmRSS")))
	if entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid)); err == nil {
		cliKV("打开文件", strconv.Itoa(len(entries)))
	}
	cliSection("路径")
	cliKV("数据目录", fvDetectDataDir(fvDefaultData))
	cliKV("安装记录", fvRecordFile)
	if exe, err := os.Executable(); err == nil {
		cliKV("当前二进制", exe)
	}
	cliSep()
	return 0
}

// fvFindProcess 返回 (运行方式, PID)。
func fvFindProcess(_ string) (string, int) {
	out, _ := exec.Command("systemctl", "show", "-p", "MainPID", "--value", fvServiceName).Output()
	if pid, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil && pid > 0 {
		if _, e := os.Stat(fmt.Sprintf("/proc/%d", pid)); e == nil {
			return "systemd (" + fvServiceName + ".service)", pid
		}
	}
	if line, err := exec.Command("docker", "ps", "--filter", "name="+fvBinName, "--format", "{{.Names}}|{{.Status}}").Output(); err == nil {
		for _, s := range strings.Split(string(line), "\n") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			parts := strings.SplitN(s, "|", 2)
			if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
				continue
			}
			if p := fvContainerPID(strings.TrimSpace(parts[0])); p > 0 {
				return "docker（容器 " + strings.TrimSpace(parts[0]) + "）", p
			}
		}
	}
	if pid := fvProcScan(); pid > 0 {
		return "直接运行（/proc）", pid
	}
	return "", 0
}

func fvContainerPID(name string) int {
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Pid}}", name).Output()
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

func fvProcScan() int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if !e.IsDir() || !fvIsNum(e.Name()) {
			continue
		}
		pid, _ := strconv.Atoi(e.Name())
		if pid <= 0 || pid == os.Getpid() {
			continue
		}
		if t, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid)); err == nil && filepath.Base(t) == fvBinName {
			return pid
		}
	}
	return 0
}

func fvRunningProcs() []fvProc {
	var out []fvProc
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() || !fvIsNum(e.Name()) {
			continue
		}
		pid, _ := strconv.Atoi(e.Name())
		if pid <= 0 {
			continue
		}
		if t, err := os.Readlink(filepath.Join("/proc", e.Name(), "exe")); err != nil || filepath.Base(t) != fvBinName {
			continue
		}
		p := fvProc{pid: pid}
		if raw, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline")); err == nil {
			cmdline := strings.ReplaceAll(string(raw), "\x00", " ")
			if m := fvDataArgRe.FindStringSubmatch(cmdline); m != nil {
				for _, g := range m[1:] {
					if g != "" {
						p.data = g
						break
					}
				}
			}
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].pid < out[j].pid })
	return out
}

func fvProcField(pid int, name string) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, name+":") {
			return strings.TrimSpace(strings.TrimPrefix(line, name+":"))
		}
	}
	return ""
}

func fvProcUptimeSec(pid int) int64 {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0
	}
	upt, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	content := string(stat)
	closeIdx := strings.LastIndex(content, ")")
	if closeIdx < 0 || closeIdx+2 >= len(content) {
		return 0
	}
	fields := strings.Fields(content[closeIdx+2:])
	if len(fields) < 20 {
		return 0
	}
	startTicks, _ := strconv.ParseInt(fields[19], 10, 64)
	start := startTicks / 100
	upSec, _ := strconv.ParseFloat(strings.Fields(string(upt))[0], 64)
	elapsed := int64(upSec) - start
	if elapsed < 0 {
		return 0
	}
	return elapsed
}

func fvFormatUptime(sec int64) string {
	if sec <= 0 {
		return "未知"
	}
	return fmt.Sprintf("%d小时 %d分钟 %d秒", sec/3600, (sec%3600)/60, sec%60)
}

func fvFormatMem(raw string) string {
	if raw == "" {
		return "未知"
	}
	parts := strings.Fields(raw)
	if len(parts) < 2 {
		return raw
	}
	kb, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return raw
	}
	mb := kb / 1024.0
	if mb >= 1024 {
		return fmt.Sprintf("%s（%.2f GB）", raw, mb/1024.0)
	}
	return fmt.Sprintf("%s（%.2f MB）", raw, mb)
}

func fvListenPort(pid int) string {
	inodes := fvSocketInodes(pid)
	if len(inodes) == 0 {
		return ""
	}
	for _, proto := range []string{"tcp", "tcp6"} {
		if p := fvPortInTCP(pid, proto, inodes); p != "" {
			return p
		}
	}
	return ""
}

func fvSocketInodes(pid int) map[int64]bool {
	dir := fmt.Sprintf("/proc/%d/fd", pid)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	inodes := make(map[int64]bool)
	for _, e := range entries {
		t, err := os.Readlink(filepath.Join(dir, e.Name()))
		if err != nil || !strings.HasPrefix(t, "socket:[") || !strings.HasSuffix(t, "]") {
			continue
		}
		num := strings.TrimSuffix(strings.TrimPrefix(t, "socket:["), "]")
		if v, err := strconv.ParseInt(num, 10, 64); err == nil {
			inodes[v] = true
		}
	}
	return inodes
}

func fvPortInTCP(pid int, proto string, inodes map[int64]bool) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/net/%s", pid, proto))
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > 0 {
		lines = lines[1:]
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 10 || fields[3] != "0A" {
			continue
		}
		inode, err := strconv.ParseInt(fields[9], 10, 64)
		if err != nil || !inodes[inode] {
			continue
		}
		parts := strings.Split(fields[1], ":")
		if len(parts) < 2 {
			continue
		}
		port, err := strconv.ParseInt(parts[1], 16, 64)
		if err != nil || port <= 0 {
			continue
		}
		return strconv.FormatInt(port, 10)
	}
	return ""
}

func fvIsNum(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func fvReadRecord(key string) (string, bool) {
	content, err := os.ReadFile(fvRecordFile)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == key {
			v = strings.TrimSpace(v)
			return v, v != ""
		}
	}
	return "", false
}

// fvDetectDataDir 解析实际数据目录：记录 > 默认值。
func fvDetectDataDir(fallback string) string {
	if dir, ok := fvReadRecord("DATA_DIR"); ok {
		return dir
	}
	return fallback
}
