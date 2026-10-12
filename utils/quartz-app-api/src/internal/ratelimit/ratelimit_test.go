package ratelimit

import (
	"os"
	"testing"
	"time"
)

func testLimiter(t *testing.T) *Limiter {
	t.Helper()
	return New(t.TempDir()+"/rl.json", Config{Fails: 3, Base: 10})
}

func TestLockoutAndBackoff(t *testing.T) {
	l := testLimiter(t)
	now := time.Unix(1760000000, 0)

	for i := 0; i < 2; i++ {
		if err := l.Fail(GlobalKey, now); err != nil {
			t.Fatal(err)
		}
	}
	if locked, _ := l.Check(GlobalKey, now); locked {
		t.Fatal("should not be locked yet")
	}
	// 第三次触发锁定 base*1 = 10s
	if err := l.Fail(GlobalKey, now); err != nil {
		t.Fatal(err)
	}
	locked, retry := l.Check(GlobalKey, now)
	if !locked || retry != 10 {
		t.Fatalf("locked=%v retry=%d", locked, retry)
	}
	// 锁定过期后解锁
	if locked, _ := l.Check(GlobalKey, now.Add(11*time.Second)); locked {
		t.Fatal("lock should expire")
	}
	// 再次触发 → 退避翻倍（20s）
	for i := 0; i < 3; i++ {
		_ = l.Fail(GlobalKey, now)
	}
	if _, retry = l.Check(GlobalKey, now); retry != 20 {
		t.Fatalf("backoff retry=%d want 20", retry)
	}
	// 退避有上限（64×）
	for i := 0; i < 40; i++ {
		for j := 0; j < 3; j++ {
			_ = l.Fail(GlobalKey, now)
		}
	}
	if _, retry = l.Check(GlobalKey, now); retry > 10*64 {
		t.Fatalf("backoff cap exceeded: %d", retry)
	}
}

func TestResetOnSuccess(t *testing.T) {
	l := testLimiter(t)
	now := time.Unix(1760000000, 0)
	_ = l.Fail(GlobalKey, now)
	_ = l.Fail(GlobalKey, now)
	if err := l.Reset(GlobalKey, now); err != nil {
		t.Fatal(err)
	}
	// 清零后需再失败 3 次才锁
	_ = l.Fail(GlobalKey, now)
	_ = l.Fail(GlobalKey, now)
	if locked, _ := l.Check(GlobalKey, now); locked {
		t.Fatal("reset did not clear")
	}
}

func TestKeysIsolated(t *testing.T) {
	l := testLimiter(t)
	now := time.Unix(1760000000, 0)
	for i := 0; i < 3; i++ {
		_ = l.Fail("a", now)
	}
	if locked, _ := l.Check("a", now); !locked {
		t.Fatal("key a should be locked")
	}
	if locked, _ := l.Check("b", now); locked {
		t.Fatal("key b must not be locked")
	}
}

func TestCorruptStateRecovers(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/rl.json"
	l := New(path, Config{Fails: 3, Base: 10})
	now := time.Unix(1760000000, 0)
	_ = l.Fail(GlobalKey, now)
	// 写坏文件
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if locked, _ := l.Check(GlobalKey, now); locked {
		t.Fatal("corrupt state should reset cleanly")
	}
}
