package auth

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/quartzwrt/quartz-app-api/internal/config"
	"github.com/quartzwrt/quartz-app-api/internal/envelope"
	"github.com/quartzwrt/quartz-app-api/internal/ratelimit"
	"github.com/quartzwrt/quartz-app-api/internal/store"
)

func newTestService(t *testing.T, ok bool) (*Service, *time.Time) {
	t.Helper()
	dir := t.TempDir()
	now := time.Unix(1760000000, 0)
	svc := New(store.New(dir), ratelimit.New(dir+"/rl.json", ratelimit.Config{Fails: 3, Base: 10}), config.Defaults(),
		func(ctx context.Context, u, p string) error {
			if ok {
				return nil
			}
			return errors.New("bad credentials")
		})
	svc.Now = func() time.Time { return now }
	svc.Router = func() map[string]any { return map[string]any{"name": "router"} }
	return svc, &now
}

func mustLogin(t *testing.T, svc *Service) map[string]any {
	t.Helper()
	p, e := svc.Login(context.Background(), "root", "pw", "iPhone", "")
	if e != nil {
		t.Fatalf("login: %v", e)
	}
	return p
}

func TestLoginIssuesTokens(t *testing.T) {
	svc, _ := newTestService(t, true)
	p := mustLogin(t, svc)
	for _, k := range []string{"access_token", "access_exp", "refresh_token", "refresh_exp", "device_id"} {
		if p[k] == nil || p[k] == "" {
			t.Fatalf("missing %s in %v", k, p)
		}
	}
	// 载荷不带信封（由调用层统一包裹，见 cmd/quartz-app-api reply()）
	if _, has := p["ok"]; has {
		t.Fatalf("auth 层不应自带信封: %v", p)
	}
	// 存储里只有哈希，没有明文 refresh
	st, _ := svc.Store.Load()
	if len(st.Devices) != 1 {
		t.Fatalf("devices=%d", len(st.Devices))
	}
	raw, _ := storeReadState(t, svc)
	if strings.Contains(raw, p["refresh_token"].(string)) {
		t.Fatal("plaintext refresh token persisted")
	}
}

func storeReadState(t *testing.T, svc *Service) (string, error) {
	t.Helper()
	b, err := os.ReadFile(svc.Store.Dir + "/state.json")
	return string(b), err
}

func TestLoginRateLimited(t *testing.T) {
	svc, now := newTestService(t, false)
	for i := 0; i < 3; i++ {
		_, e := svc.Login(context.Background(), "root", "bad", "d", "")
		if e == nil || e.Code != envelope.CodeInvalidCredentials {
			t.Fatalf("attempt %d: %v", i, e)
		}
	}
	// 第 4 次应被限速
	_, e := svc.Login(context.Background(), "root", "bad", "d", "")
	if e == nil || e.Code != envelope.CodeRateLimited || e.RetryAfter != 10 {
		t.Fatalf("want rate_limited retry=10, got %+v", e)
	}
	// 锁定到期后恢复（仍是错误密码 → invalid_credentials）
	*now = now.Add(11 * time.Second)
	_, e = svc.Login(context.Background(), "root", "bad", "d", "")
	if e == nil || e.Code != envelope.CodeInvalidCredentials {
		t.Fatalf("after lock expiry: %+v", e)
	}
}

func TestRefreshRotationAndReuse(t *testing.T) {
	svc, _ := newTestService(t, true)
	p := mustLogin(t, svc)
	dev := p["device_id"].(string)
	oldRefresh := p["refresh_token"].(string)

	// 正常旋转
	p2, e := svc.Refresh(dev, oldRefresh)
	if e != nil {
		t.Fatalf("refresh: %v", e)
	}
	newRefresh := p2["refresh_token"].(string)
	if newRefresh == oldRefresh {
		t.Fatal("refresh token not rotated")
	}
	if p2["access_token"] == p["access_token"] {
		t.Fatal("access token not renewed")
	}

	// 旧 refresh 再用 → 复用检测 → 吊销
	_, e = svc.Refresh(dev, oldRefresh)
	if e == nil || e.Code != envelope.CodeDeviceRevoked {
		t.Fatalf("reuse should revoke: %+v", e)
	}
	// 设备已吊销：新 refresh 也无法再用
	_, e = svc.Refresh(dev, newRefresh)
	if e == nil || e.Code != envelope.CodeDeviceRevoked {
		t.Fatalf("revoked device refresh: %+v", e)
	}
	// 新签的 access 立即失效（吊销即时生效）
	_, e = svc.Authenticate(p2["access_token"].(string))
	if e == nil || e.Code != envelope.CodeDeviceRevoked {
		t.Fatalf("authenticate after revoke: %+v", e)
	}
}

func TestRefreshGarbageAndUnknownDevice(t *testing.T) {
	svc, _ := newTestService(t, true)
	p := mustLogin(t, svc)
	dev := p["device_id"].(string)

	if _, e := svc.Refresh(dev, "not-a-token"); e == nil || e.Code != envelope.CodeTokenInvalid {
		t.Fatalf("garbage refresh: %+v", e)
	}
	if _, e := svc.Refresh("unknown", p["refresh_token"].(string)); e == nil || e.Code != envelope.CodeTokenInvalid {
		t.Fatalf("unknown device: %+v", e)
	}
}

func TestRefreshAbsoluteCap(t *testing.T) {
	svc, now := newTestService(t, true)
	p := mustLogin(t, svc)
	dev := p["device_id"].(string)
	refresh := p["refresh_token"].(string)

	// 把设备 Created 拨到绝对上限之外
	svc.Cfg.RefreshAbsTTL = 100
	_, _ = svc.Store.Update(func(st *store.State) error {
		st.Find(dev).Created = now.Unix() - 200
		return nil
	})
	if _, e := svc.Refresh(dev, refresh); e == nil || e.Code != envelope.CodeTokenInvalid {
		t.Fatalf("absolute cap: %+v", e)
	}
}

func TestAuthenticateExpiredAndTampered(t *testing.T) {
	svc, now := newTestService(t, true)
	p := mustLogin(t, svc)
	access := p["access_token"].(string)

	// 未过期可用
	if _, e := svc.Authenticate(access); e != nil {
		t.Fatalf("fresh access: %v", e)
	}
	// 超时（>15min+5min skew）
	*now = now.Add(21 * time.Minute)
	if _, e := svc.Authenticate(access); e == nil || e.Code != envelope.CodeTokenExpired {
		t.Fatalf("expired: %+v", e)
	}
	// 篡改
	*now = time.Unix(1760000000, 0)
	if _, e := svc.Authenticate(access[:len(access)-2] + "AA"); e == nil || e.Code != envelope.CodeTokenInvalid {
		t.Fatalf("tampered: %+v", e)
	}
}

func TestLogout(t *testing.T) {
	svc, _ := newTestService(t, true)
	p := mustLogin(t, svc)
	dev := p["device_id"].(string)
	refresh := p["refresh_token"].(string)

	if _, e := svc.Logout(dev, refresh); e != nil {
		t.Fatalf("logout: %v", e)
	}
	st, _ := svc.Store.Load()
	if len(st.Devices) != 0 {
		t.Fatal("device not removed")
	}
	// 登出后 refresh 无效
	if _, e := svc.Refresh(dev, refresh); e == nil || e.Code != envelope.CodeTokenInvalid {
		t.Fatalf("refresh after logout: %+v", e)
	}
	// 错误 refresh 不能登出
	p2 := mustLogin(t, svc)
	if _, e := svc.Logout(p2["device_id"].(string), "wrong"); e == nil {
		t.Fatal("logout with wrong token must fail")
	}
}

func TestListAndRevoke(t *testing.T) {
	svc, _ := newTestService(t, true)
	a := mustLogin(t, svc)
	b := mustLogin(t, svc)
	devA, devB := a["device_id"].(string), b["device_id"].(string)

	list, e := svc.ListDevices(devB)
	if e != nil || len(list) != 2 {
		t.Fatalf("list: %v %v", list, e)
	}
	cur := 0
	for _, d := range list {
		if d["current"] == true {
			cur++
		}
	}
	if cur != 1 {
		t.Fatalf("current flags: %v", list)
	}

	// A 吊销 B
	if e := svc.Revoke(devA, devB); e != nil {
		t.Fatalf("revoke: %v", e)
	}
	if _, e := svc.Authenticate(b["access_token"].(string)); e == nil || e.Code != envelope.CodeDeviceRevoked {
		t.Fatalf("B should be revoked: %+v", e)
	}
	if _, e := svc.Authenticate(a["access_token"].(string)); e != nil {
		t.Fatalf("A should still work: %v", e)
	}
	// 不存在的设备
	if e := svc.Revoke(devA, "nope"); e == nil || e.Code != envelope.CodeBadRequest {
		t.Fatalf("revoke unknown: %+v", e)
	}
}

func TestSanitizeName(t *testing.T) {
	long := strings.Repeat("x", 200)
	cases := map[string]string{
		"":            "device",
		"  iPhone  ":  "iPhone",
		"bad\x00name": "badname",
		long:          long[:96],
	}
	for in, want := range cases {
		if got := sanitizeName(in); got != want {
			t.Fatalf("sanitizeName(%q)=%q want %q", in, got, want)
		}
	}
}

func TestSanitizeIP(t *testing.T) {
	for _, ok := range []string{"192.168.1.23", "fe80::1%br-lan", "2001:db8::1"} {
		if got := sanitizeIP(ok); got != ok {
			t.Fatalf("valid ip rejected: %q -> %q", ok, got)
		}
	}
	for _, bad := range []string{"192.168.1.1; rm -rf /", "abc", strings.Repeat("1", 46), "999.1.1.1", ""} {
		if got := sanitizeIP(bad); got != "" {
			t.Fatalf("bad ip accepted: %q -> %q", bad, got)
		}
	}
}

func TestRootPasswordSet(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty shadow", "", false},
		{"no root entry", "daemon:*:1:1:99999:7:::\n", false},
		{"root empty password", "root::0:0:99999:7:::\n", false},
		{"root locked", "root:!::0:99999:7:::\n", false},
		{"root locked star", "root:*:0:0:99999:7:::\n", false},
		{"root locked hash", "root:!$6$abc$def:0:0:99999:7:::\n", false},
		{"root sha512", "root:$6$salt$hashvalue:0:0:99999:7:::\n", true},
		{"root des", "root:aBcDeFgHiJkLm:0:0:99999:7:::\n", true},
		{"multi-entry", "daemon:*:1:1:99999:7:::\nroot:$6$x$y:0:0:99999:7:::\nnobody:*:2:2:99999:7:::\n", true},
	}
	for _, tc := range cases {
		if got := RootPasswordSet(tc.content); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}
