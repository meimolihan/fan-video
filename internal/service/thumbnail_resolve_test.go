package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePosterFileExtensionDrift(t *testing.T) {
	dir := t.TempDir()

	exact := filepath.Join(dir, "poster.jpg")
	if err := os.WriteFile(exact, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	webp := filepath.Join(dir, "webp_only.webp")
	if err := os.WriteFile(webp, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	// 记录为 .jpeg 但磁盘上是 .webp：应解析到真实存在的 .webp 文件。
	if got := resolvePosterFile(filepath.Join(dir, "webp_only.jpeg")); got != webp {
		t.Fatalf("extension drift: got %q, want %q", got, webp)
	}

	// 精确路径存在：应原样返回。
	if got := resolvePosterFile(exact); got != exact {
		t.Fatalf("exact path: got %q, want %q", got, exact)
	}

	// 完全不存在的文件：应原样返回（交由调用方报错），而不是 panic。
	missing := filepath.Join(dir, "nope.png")
	if got := resolvePosterFile(missing); got != missing {
		t.Fatalf("missing path: got %q, want %q", got, missing)
	}

	// 空路径应原样返回。
	if got := resolvePosterFile(""); got != "" {
		t.Fatalf("empty path: got %q", got)
	}
}

func TestResolvePosterFileUpperCaseExtension(t *testing.T) {
	dir := t.TempDir()
	jpg := filepath.Join(dir, "a.JPG")
	if err := os.WriteFile(jpg, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := resolvePosterFile(filepath.Join(dir, "a.jpeg")); got != jpg {
		t.Fatalf("case fallback: got %q, want %q", got, jpg)
	}
}
