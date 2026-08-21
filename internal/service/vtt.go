package service

import (
	"fmt"
	"strings"
)

// vttCue VTT 字幕条目
type vttCue struct {
	startTime string
	endTime   string
	text      string
}

// parseVTTCues 解析 VTT 文件中的字幕条目
func parseVTTCues(content string) []vttCue {
	var cues []vttCue
	lines := strings.Split(content, "\n")

	i := 0
	// 跳过 WEBVTT 头部
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) == "" {
			i++
			break
		}
		i++
	}

	for i < len(lines) {
		line := strings.TrimSpace(lines[i])
		i++

		// 跳过空行和序号行
		if line == "" {
			continue
		}

		// 查找时间戳行
		if !strings.Contains(line, "-->") {
			// 可能是序号行，继续找时间戳
			if i < len(lines) && strings.Contains(lines[i], "-->") {
				line = strings.TrimSpace(lines[i])
				i++
			} else {
				continue
			}
		}

		// 解析时间戳
		parts := strings.Split(line, "-->")
		if len(parts) != 2 {
			continue
		}

		startTime := strings.TrimSpace(parts[0])
		endTime := strings.TrimSpace(parts[1])

		// 收集文本行
		var textLines []string
		for i < len(lines) {
			textLine := strings.TrimSpace(lines[i])
			if textLine == "" {
				i++
				break
			}
			textLines = append(textLines, textLine)
			i++
		}

		if len(textLines) > 0 {
			cues = append(cues, vttCue{
				startTime: startTime,
				endTime:   endTime,
				text:      strings.Join(textLines, "\n"),
			})
		}
	}

	return cues
}

// formatVTTTime 将秒数格式化为 VTT 时间戳 "HH:MM:SS.mmm"
func formatVTTTime(seconds float64) string {
	h := int(seconds) / 3600
	m := (int(seconds) % 3600) / 60
	sec := int(seconds) % 60
	ms := int((seconds - float64(int(seconds))) * 1000)
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, sec, ms)
}
