package history

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestAddAndRing(t *testing.T) {
	s := New(t.TempDir())
	for i := 0; i < Cap+5; i++ {
		if err := s.Add(Record{Action: "packages.refresh", OK: true, TS: int64(i + 1)}); err != nil {
			t.Fatal(err)
		}
	}
	list := s.List(0)
	if len(list) != Cap {
		t.Fatalf("ring: got %d want %d", len(list), Cap)
	}
	// 最新在前，最旧保留 TS=6（0..Cap+4 → 保留 6..35）
	if list[0].TS != int64(Cap+5) || list[Cap-1].TS != 6 {
		t.Fatalf("order: first=%d last=%d", list[0].TS, list[Cap-1].TS)
	}
	// 文件 0600
	fi, err := os.Stat(filepath.Join(s.Dir, "history.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode: %v", fi.Mode())
	}
}

func TestOutputTruncationKeepsTail(t *testing.T) {
	s := New(t.TempDir())
	long := strings.Repeat("x", OutputMax) + "TAIL-MARKER"
	if err := s.Add(Record{Action: "packages.upgrade", Output: long}); err != nil {
		t.Fatal(err)
	}
	list := s.List(1)
	if len(list[0].Output) > OutputMax || !strings.HasSuffix(list[0].Output, "TAIL-MARKER") {
		t.Fatalf("truncation: len=%d tail=%q", len(list[0].Output), list[0].Output[len(list[0].Output)-12:])
	}
}

func TestCorruptFileTreatedAsEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "history.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(dir)
	if got := s.List(0); got != nil {
		t.Fatalf("corrupt should be empty: %v", got)
	}
	// 且可继续追加（覆盖损坏文件）
	if err := s.Add(Record{Action: "packages.remove", Target: "x", OK: false}); err != nil {
		t.Fatal(err)
	}
	if got := s.List(0); len(got) != 1 {
		t.Fatalf("after add: %v", got)
	}
}

func TestConcurrentAddsAreSerialized(t *testing.T) {
	s := New(t.TempDir())
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = s.Add(Record{Action: "packages.refresh", OK: true, TS: int64(i + 1)})
		}(i)
	}
	wg.Wait()
	list := s.List(0)
	if len(list) != 20 {
		t.Fatalf("concurrent adds lost records: %d", len(list))
	}
	// 无重复 TS（每个 Add 都成功落盘）
	seen := map[int64]bool{}
	for _, r := range list {
		if seen[r.TS] {
			t.Fatalf("duplicate TS %d", r.TS)
		}
		seen[r.TS] = true
	}
}
