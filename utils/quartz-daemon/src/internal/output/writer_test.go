package output

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	w := New(filepath.Join(dir, "status.json"))

	if err := w.Write([]byte(`{"v":1}`)); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "status.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"v":1}` {
		t.Fatalf("content = %q", b)
	}
	fi, err := os.Stat(filepath.Join(dir, "status.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %o, want 0644", fi.Mode().Perm())
	}

	// 无临时文件残留
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("leftover files: %v", entries)
	}

	// 覆盖写
	if err := w.Write([]byte(`{"v":2}`)); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(dir, "status.json"))
	if string(b) != `{"v":2}` {
		t.Fatalf("overwrite content = %q", b)
	}
}

func TestEnsureDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	if err := EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("dir not created: %v", err)
	}
}
