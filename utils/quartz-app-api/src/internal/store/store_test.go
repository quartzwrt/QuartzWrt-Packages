package store

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestSecretCreateAndReuse(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	a, err := s.Secret()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 32 {
		t.Fatalf("secret len=%d", len(a))
	}
	// 复读一致
	b, err := s.Secret()
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("secret not stable")
	}
	// 权限 0600
	fi, err := os.Stat(filepath.Join(dir, "secret"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("secret perm=%v", fi.Mode().Perm())
	}
}

func TestSecretConcurrentCreate(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	var wg sync.WaitGroup
	results := make([][]byte, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b, err := s.Secret()
			if err != nil {
				t.Errorf("goroutine %d: %v", i, err)
				return
			}
			results[i] = b
		}(i)
	}
	wg.Wait()
	for i := 1; i < len(results); i++ {
		if string(results[i]) != string(results[0]) {
			t.Fatalf("concurrent secret mismatch: %x vs %x", results[i], results[0])
		}
	}
}

func TestStateUpdateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	st, err := s.Update(func(st *State) error {
		st.Devices = append(st.Devices, Device{ID: "d1", Name: "iPhone", RefreshSHA256: "abc", RefreshExp: 123})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Devices) != 1 {
		t.Fatalf("devices=%d", len(st.Devices))
	}
	// 复读
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Find("d1") == nil || got.Find("d1").Name != "iPhone" {
		t.Fatalf("reload mismatch: %+v", got)
	}
	// 权限 0600
	fi, _ := os.Stat(filepath.Join(dir, "state.json"))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("state perm=%v", fi.Mode().Perm())
	}
	// fn 报错不落盘
	if _, err := s.Update(func(st *State) error {
		st.Devices = nil
		return os.ErrInvalid
	}); err == nil {
		t.Fatal("want error")
	}
	got2, _ := s.Load()
	if len(got2.Devices) != 1 {
		t.Fatal("failed update must not persist")
	}
}

func TestStateConcurrentUpdates(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Update(func(st *State) error {
				st.Devices = append(st.Devices, Device{ID: NewDeviceID()})
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Devices) != 16 {
		t.Fatalf("lost updates: %d/16（flock 失效）", len(got.Devices))
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	s := New(t.TempDir())
	st, err := s.Load()
	if err != nil || len(st.Devices) != 0 {
		t.Fatalf("err=%v st=%+v", err, st)
	}
}
