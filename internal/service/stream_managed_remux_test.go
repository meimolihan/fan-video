package service

import (
	"testing"
)

func TestManagedRemuxModeCopiesCompatibleAudio(t *testing.T) {
	for _, codec := range []string{"aac", "mp3"} {
		mode, ok := managedRemuxMode(codec)
		if !ok || mode != ManagedRemuxCopyAudio {
			t.Fatalf("audio=%q mode=%q ok=%v", codec, mode, ok)
		}
	}
}

func TestManagedRemuxModeTranscodesOnlyIncompatibleAudio(t *testing.T) {
	// 空编码（未知）同样必须转码，不能盲目 copy：文件可能带 AC3/DTS/TrueHD，
	// 浏览器无法解码，会造成"有画面没声音"。
	for _, codec := range []string{"", "dts", "truehd", "flac", "opus", "ac3", "eac3"} {
		mode, ok := managedRemuxMode(codec)
		if !ok || mode != ManagedRemuxTranscodeAudio {
			t.Fatalf("audio=%q mode=%q ok=%v", codec, mode, ok)
		}
	}
}
