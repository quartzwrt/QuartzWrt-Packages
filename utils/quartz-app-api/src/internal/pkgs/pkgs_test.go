package pkgs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/quartzwrt/quartz-app-api/internal/envelope"
)

// fakeRunner 记录调用并按参数返回预设输出（key = 首个非选项参数组合的签名）。
type fakeRunner struct {
	calls [][]string
	reply func(args []string) ([]byte, error)
}

func (f *fakeRunner) run(_ context.Context, _ string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	if f.reply != nil {
		return f.reply(args)
	}
	return []byte("[]"), nil
}

func newTestManager(t *testing.T, reply func(args []string) ([]byte, error)) (*Manager, *fakeRunner) {
	t.Helper()
	f := &fakeRunner{reply: reply}
	m := New(t.TempDir())
	m.Run = f.run
	return m, f
}

const installedJSON = `[
 {"name":"bash","version":"5.3-r4","description":"Bash","arch":"x86_64","license":"GPL","origin":"feeds/packages/bash","installed-size":1009993,"file-size":504638},
 {"name":"ttyd","version":"1.7.7-r1","description":"ttyd","arch":"x86_64","license":"MIT","origin":"feeds/packages/ttyd","installed-size":620768,"file-size":408255}
]`

const upgradableJSON = `[
 {"name":"ttyd","version":"1.7.8-r1","description":"ttyd","arch":"x86_64","license":"MIT","origin":"feeds/packages/ttyd","installed-size":621000,"file-size":408300}
]`

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// itemsOf 取分页条目（scope=包列表时 Items 为 []Item）。
func itemsOf(t *testing.T, pg *Page) []Item {
	t.Helper()
	items, ok := pg.Items.([]Item)
	if !ok {
		t.Fatalf("items type: %T", pg.Items)
	}
	return items
}

func TestListInstalledMarksUpgradable(t *testing.T) {
	m, f := newTestManager(t, func(args []string) ([]byte, error) {
		if hasArg(args, "--upgradable") {
			return []byte("WARNING: something\n" + upgradableJSON), nil
		}
		return []byte(installedJSON), nil
	})
	pg, errE := m.List(context.Background(), "installed", "", 1, 10)
	if errE != nil {
		t.Fatalf("list: %v", errE)
	}
	items := itemsOf(t, pg)
	if pg.Total != 2 || len(items) != 2 || items[0].Name != "bash" || items[1].Name != "ttyd" {
		t.Fatalf("page: %+v", pg)
	}
	if items[0].Upgradable {
		t.Fatalf("bash should not be upgradable")
	}
	if !items[0].Installed || !items[1].Installed {
		t.Fatalf("installed scope items must be flagged installed: %+v", items)
	}
	if !items[1].Upgradable || items[1].TargetVersion != "1.7.8-r1" {
		t.Fatalf("ttyd should be upgradable to 1.7.8-r1: %+v", items[1])
	}
	// 首次调用须是 installed 查询（含 --installed/--from system）
	if len(f.calls) < 1 || !hasArg(f.calls[0], "--installed") || !hasArg(f.calls[0], "system") {
		t.Fatalf("installed args: %v", f.calls[0])
	}
	if items[1].InstalledSize != 620768 || items[1].FileSize != 408255 {
		t.Fatalf("sizes: %+v", items[1])
	}
}

func TestListUpgradableMergesInstalledVersion(t *testing.T) {
	m, _ := newTestManager(t, func(args []string) ([]byte, error) {
		if hasArg(args, "--upgradable") {
			return []byte(upgradableJSON), nil
		}
		return []byte(installedJSON), nil
	})
	pg, errE := m.List(context.Background(), "upgradable", "", 1, 10)
	if errE != nil {
		t.Fatalf("list: %v", errE)
	}
	up := itemsOf(t, pg)
	if pg.Total != 1 || up[0].Version != "1.7.8-r1" || up[0].InstalledVersion != "1.7.7-r1" || !up[0].Upgradable {
		t.Fatalf("upgradable item: %+v", up[0])
	}
}

func TestListAvailableMergesInstalledFlag(t *testing.T) {
	m, _ := newTestManager(t, func(args []string) ([]byte, error) {
		if hasArg(args, "--available") {
			return []byte(`[
			  {"name":"ttyd","version":"1.7.7-r1","file-size":408255},
			  {"name":"nano","version":"8.0-r1","file-size":120000},
			  {"name":"bash","version":"5.4-r1","file-size":505000}
			]`), nil
		}
		return []byte(installedJSON), nil
	})
	pg, errE := m.List(context.Background(), "available", "t", 1, 10)
	if errE != nil {
		t.Fatalf("list: %v", errE)
	}
	byName := map[string]Item{}
	for _, it := range itemsOf(t, pg) {
		byName[it.Name] = it
	}
	if !byName["ttyd"].Installed || byName["ttyd"].Upgradable {
		t.Fatalf("ttyd: same version installed, no upgrade: %+v", byName["ttyd"])
	}
	if !byName["bash"].Installed || !byName["bash"].Upgradable || byName["bash"].InstalledVersion != "5.3-r4" {
		t.Fatalf("bash: installed older, upgradable: %+v", byName["bash"])
	}
	if byName["nano"].Installed || byName["nano"].Upgradable {
		t.Fatalf("nano: not installed: %+v", byName["nano"])
	}
}

func TestListSearchPatternWrapping(t *testing.T) {
	m, f := newTestManager(t, func(args []string) ([]byte, error) { return []byte("[]"), nil })
	if _, errE := m.List(context.Background(), "available", "Tail-Scale", 1, 10); errE != nil {
		t.Fatalf("list: %v", errE)
	}
	var avail []string
	for _, c := range f.calls {
		if hasArg(c, "--available") {
			avail = c
		}
	}
	if avail == nil || !hasArg(avail, "*tail-scale*") {
		t.Fatalf("pattern not wrapped/lowered: %v", f.calls)
	}
	// 非法字符（apk 模式通配 / shell 注入尝试）必须拒绝
	for _, q := range []string{"a*", "a b", "a/../b", ";rm", strings.Repeat("x", 65), "a$b"} {
		if _, errE := m.List(context.Background(), "available", q, 1, 10); errE == nil || errE.Code != envelope.CodeBadRequest {
			t.Fatalf("query %q should be bad_request, got %+v", q, errE)
		}
	}
}

func TestListPagination(t *testing.T) {
	m, _ := newTestManager(t, func(args []string) ([]byte, error) {
		if hasArg(args, "--installed") {
			// 60 个包（a00..a59）
			var b strings.Builder
			b.WriteString("[")
			for i := 0; i < 60; i++ {
				if i > 0 {
					b.WriteString(",")
				}
				b.WriteString(`{"name":"a` + string(rune('0'+i/10)) + string(rune('0'+i%10)) + `","version":"1"}`)
			}
			b.WriteString("]")
			return []byte(b.String()), nil
		}
		return []byte("[]"), nil
	})
	pg, errE := m.List(context.Background(), "installed", "", 2, 25)
	if errE != nil {
		t.Fatalf("list: %v", errE)
	}
	if pg.Total != 60 || pg.Pages != 3 || pg.Page != 2 || len(itemsOf(t, pg)) != 25 {
		t.Fatalf("page meta: %+v", pg)
	}
	if itemsOf(t, pg)[0].Name != "a25" {
		t.Fatalf("page 2 first item: %+v", itemsOf(t, pg)[0])
	}
	// 越界页夹紧到末页
	pg, _ = m.List(context.Background(), "installed", "", 99, 25)
	if pg.Page != 3 || len(itemsOf(t, pg)) != 10 {
		t.Fatalf("clamp: %+v", pg)
	}
	// per_page 上限
	pg, _ = m.List(context.Background(), "installed", "", 1, 9999)
	if pg.PerPage != MaxPerPage {
		t.Fatalf("per_page cap: %+v", pg)
	}
}

func TestListUnknownScopeAndBadJSON(t *testing.T) {
	m, _ := newTestManager(t, func(args []string) ([]byte, error) { return []byte("nonsense"), nil })
	if _, errE := m.List(context.Background(), "whatever", "", 1, 10); errE == nil || errE.Code != envelope.CodeBadRequest {
		t.Fatalf("unknown scope: %+v", errE)
	}
	if _, errE := m.List(context.Background(), "installed", "", 1, 10); errE == nil || errE.Code != envelope.CodeDataUnavailable {
		t.Fatalf("bad json: %+v", errE)
	}
	// apk 报错（exit != 0）→ data_unavailable + ERROR 行摘录
	m2, _ := newTestManager(t, func(args []string) ([]byte, error) {
		return []byte("ERROR: Not continuing due to stale/unavailable repositories."), errors.New("exit 99")
	})
	if _, errE := m2.List(context.Background(), "installed", "", 1, 10); errE == nil || errE.Code != envelope.CodeDataUnavailable || !strings.Contains(errE.Detail, "stale/unavailable") {
		t.Fatalf("apk error: %+v", errE)
	}
}

func TestActionsCommandConstruction(t *testing.T) {
	cases := []struct {
		action string
		name   string
		want   []string
	}{
		{"packages.refresh", "", []string{"update"}},
		{"packages.upgrade", "", []string{"upgrade"}},
		{"packages.upgrade", "ttyd", []string{"upgrade", "ttyd"}},
		{"packages.remove", "ttyd", []string{"del", "ttyd"}},
	}
	for _, c := range cases {
		m, f := newTestManager(t, func(args []string) ([]byte, error) { return []byte("OK"), nil })
		if _, errE := m.Action(context.Background(), c.action, c.name); errE != nil {
			t.Fatalf("%s %s: %v", c.action, c.name, errE)
		}
		got := f.calls[0]
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Fatalf("%s: want %v got %v", c.action, c.want, got)
		}
	}
}

func TestActionValidation(t *testing.T) {
	m, f := newTestManager(t, func(args []string) ([]byte, error) { return []byte("OK"), nil })
	bad := []struct{ action, name string }{
		{"packages.remove", ""},                        // 必填
		{"packages.remove", "ttyd; rm -rf /"},          // 注入
		{"packages.remove", "../etc/passwd"},           // 穿越
		{"packages.remove", "-f"},                      // 选项注入
		{"packages.upgrade", "ttyd --force-overwrite"}, // 参数注入
		{"packages.upgrade", strings.Repeat("a", 129)},
		{"packages.install", "x"}, // 未注册（安装浏览 v1 后置）
	}
	for _, c := range bad {
		_, errE := m.Action(context.Background(), c.action, c.name)
		if errE == nil || (errE.Code != envelope.CodeForbiddenAction && errE.Code != envelope.CodeBadRequest) {
			t.Fatalf("%s %q should be rejected, got %+v", c.action, c.name, errE)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("no execution expected: %v", f.calls)
	}
}

func TestActionFailureRecordsHistory(t *testing.T) {
	m, _ := newTestManager(t, func(args []string) ([]byte, error) {
		return []byte("ERROR: unable to select packages:\n  nano (no such package):\n    required by: world[nano]"), errors.New("exit 3")
	})
	_, errE := m.Action(context.Background(), "packages.upgrade", "nano")
	if errE == nil || errE.Code != envelope.CodeActionFailed || !strings.Contains(errE.Detail, "no such package") {
		t.Fatalf("action failure: %+v", errE)
	}
	recs := m.Hist.List(30)
	if len(recs) != 1 || recs[0].OK || recs[0].Action != "packages.upgrade" || recs[0].Target != "nano" {
		t.Fatalf("history: %+v", recs)
	}
}

func TestActionSuccessAndHistoryRing(t *testing.T) {
	m, _ := newTestManager(t, func(args []string) ([]byte, error) {
		return []byte("(1/1) Upgrading ttyd (1.7.7-r1 -> 1.7.8-r1)"), nil
	})
	if _, errE := m.Action(context.Background(), "packages.upgrade", "ttyd"); errE != nil {
		t.Fatalf("upgrade: %v", errE)
	}
	if _, errE := m.Action(context.Background(), "packages.refresh", ""); errE != nil {
		t.Fatalf("refresh: %v", errE)
	}
	// 环形：再写 28 条（总 30，upgrade 恰好保留在最旧位）
	for i := 0; i < 28; i++ {
		_, _ = m.Action(context.Background(), "packages.refresh", "")
	}
	pg, errE := m.List(context.Background(), "history", "", 1, 10)
	if errE != nil {
		t.Fatalf("history: %v", errE)
	}
	if pg.Total != 30 || pg.Pages != 1 {
		t.Fatalf("ring meta: total=%d pages=%d", pg.Total, pg.Pages)
	}
	ops, okc := pg.Items.([]Op)
	if !okc || len(ops) != 30 {
		t.Fatalf("history items type/len: %T %d", pg.Items, len(ops))
	}
	// 最新在前（最旧=upgrade 在末位）；字段显式（action/target/ok/output/duration_ms/ts）
	if ops[0].Action != "packages.refresh" || ops[29].Action != "packages.upgrade" || ops[29].Target != "ttyd" {
		t.Fatalf("order: first=%s last=%s/%s", ops[0].Action, ops[29].Action, ops[29].Target)
	}
	if !ops[29].OK || ops[29].TS == 0 || !strings.Contains(ops[29].Output, "Upgrading ttyd") {
		t.Fatalf("record fields: %+v", ops[29])
	}
}

func TestActionTimeoutMapsToFailed(t *testing.T) {
	m, _ := newTestManager(t, nil)
	m.Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		<-ctx.Done() // 模拟动作挂满预算（外层用短预算父 ctx，测试不真等 45s）
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, errE := m.Action(ctx, "packages.refresh", "")
	if errE == nil || errE.Code != envelope.CodeActionFailed || !strings.Contains(errE.Detail, "timeout") {
		t.Fatalf("timeout: %+v", errE)
	}
}
