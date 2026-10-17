package ubusfwd

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/quartzwrt/quartz-app-api/internal/audit"
	"github.com/quartzwrt/quartz-app-api/internal/envelope"
)

// capture 记录一次 ubus 调用并返回固定应答。
type capture struct {
	argv []string
	raw  string
	err  error
}

func (c *capture) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	c.argv = append([]string{name}, args...)
	return []byte(c.raw), c.err
}

func newMgr(c *capture) *Manager { return &Manager{Run: c.run} }

func TestHandles(t *testing.T) {
	for _, id := range []string{"bandix.limit_default_set", "bandix.whitelist_add",
		"bandix.whitelist_delete", "bandix.whitelist_enabled", "bandix.hostname_set"} {
		if !Handles(id) {
			t.Fatalf("Handles(%q) = false", id)
		}
	}
	for _, id := range []string{"bandix.status", "bandix.installUpdate", "packages.refresh", "", "bandix"} {
		if Handles(id) {
			t.Fatalf("Handles(%q) = true（不应注册）", id)
		}
	}
}

func TestExecuteUnknownAction(t *testing.T) {
	m := newMgr(&capture{raw: `{"ok":true}`})
	_, e := m.Execute(context.Background(), "bandix.status", nil)
	if e == nil || e.Code != envelope.CodeForbiddenAction {
		t.Fatalf("err = %+v, want forbidden_action", e)
	}
}

func TestExecuteArgValidation(t *testing.T) {
	m := newMgr(&capture{raw: `{"ok":true}`})
	cases := []struct {
		name   string
		id     string
		args   map[string]string
		wantCD string
	}{
		{"missing required", "bandix.whitelist_add", map[string]string{}, envelope.CodeBadRequest},
		{"bad mac", "bandix.whitelist_add", map[string]string{"mac": "aa:bb:cc:dd:ee"}, envelope.CodeBadRequest},
		{"mac injection", "bandix.whitelist_add", map[string]string{"mac": "aa:bb:cc:dd:ee:ff;rm -rf /"}, envelope.CodeBadRequest},
		{"unknown arg", "bandix.whitelist_add", map[string]string{"mac": "aa:bb:cc:dd:ee:ff", "extra": "1"}, envelope.CodeForbiddenAction},
		{"non digit rate", "bandix.limit_default_set", map[string]string{"wan_rx_rate_limit": "1e5", "wan_tx_rate_limit": "0"}, envelope.CodeBadRequest},
		{"negative rate", "bandix.limit_default_set", map[string]string{"wan_rx_rate_limit": "-1", "wan_tx_rate_limit": "0"}, envelope.CodeBadRequest},
		{"rate out of range", "bandix.limit_default_set", map[string]string{"wan_rx_rate_limit": "1099511627777", "wan_tx_rate_limit": "0"}, envelope.CodeBadRequest},
		{"enabled out of range", "bandix.whitelist_enabled", map[string]string{"enabled": "2"}, envelope.CodeBadRequest},
		{"bad hostname", "bandix.hostname_set", map[string]string{"mac": "aa:bb:cc:dd:ee:ff", "hostname": "bad host"}, envelope.CodeBadRequest},
	}
	for _, tc := range cases {
		_, e := m.Execute(context.Background(), tc.id, tc.args)
		if e == nil || e.Code != tc.wantCD {
			t.Fatalf("%s: err = %+v, want %s", tc.name, e, tc.wantCD)
		}
	}
}

// 数值方法：参数收进 data 对象（bridge 入参策略），数值以 JSON number 编码；
// MAC 归一化小写；空 hostname 允许（清除绑定）。
func TestExecuteWrapping(t *testing.T) {
	c := &capture{raw: `{"ok":true,"api":1,"data":null}`}
	m := newMgr(c)

	if _, e := m.Execute(context.Background(), "bandix.limit_default_set",
		map[string]string{"wan_rx_rate_limit": "102400", "wan_tx_rate_limit": "51200"}); e != nil {
		t.Fatalf("limit_default_set: %v", e)
	}
	wantArgv := []string{"ubus", "call", "quartz.bandix", "limit_default_set",
		`{"data":{"wan_rx_rate_limit":102400,"wan_tx_rate_limit":51200}}`}
	if strings.Join(c.argv, " ") != strings.Join(wantArgv, " ") {
		t.Fatalf("argv = %v", c.argv)
	}

	if _, e := m.Execute(context.Background(), "bandix.hostname_set",
		map[string]string{"mac": "AA:BB:CC:DD:EE:FF", "hostname": ""}); e != nil {
		t.Fatalf("hostname_set: %v", e)
	}
	if c.argv[3] != "hostname_set" || c.argv[4] != `{"hostname":"","mac":"aa:bb:cc:dd:ee:ff"}` {
		t.Fatalf("flat payload = %v", c.argv)
	}
}

// 上游信封成功：data 透传（写面常见 data:null，不得当失败）。
func TestExecuteUpstreamOK(t *testing.T) {
	var seen string
	m := &Manager{Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		seen = args[len(args)-1]
		return []byte(`{"ok":true,"api":1,"data":null}`), nil
	}}
	res, e := m.Execute(context.Background(), "bandix.whitelist_enabled", map[string]string{"enabled": "1"})
	if e != nil {
		t.Fatalf("err = %v", e)
	}
	if res["action"] != "bandix.whitelist_enabled" {
		t.Fatalf("result = %+v", res)
	}
	if seen != `{"data":{"enabled":1}}` {
		t.Fatalf("payload = %s", seen)
	}
}

// 上游信封失败：错误码映射（bad_request / data_unavailable / 其余 action_failed），
// message 带出上游原因。
func TestExecuteUpstreamError(t *testing.T) {
	cases := []struct {
		raw      string
		wantCode string
		wantMsg  string
	}{
		{`{"ok":false,"api":1,"error":{"code":"bad_request","message":"invalid mac"}}`, envelope.CodeBadRequest, "invalid mac"},
		{`{"ok":false,"api":1,"error":{"code":"data_unavailable","message":"Traffic monitoring is not enabled"}}`, envelope.CodeDataUnavailable, "Traffic monitoring is not enabled"},
		{`{"ok":false,"api":1,"error":{"code":"action_failed","message":"upstream write failed"}}`, envelope.CodeActionFailed, "upstream write failed"},
		{`not json`, envelope.CodeActionFailed, "bad upstream response"},
	}
	for _, tc := range cases {
		m := newMgr(&capture{raw: tc.raw})
		_, e := m.Execute(context.Background(), "bandix.whitelist_add", map[string]string{"mac": "aa:bb:cc:dd:ee:ff"})
		if e == nil || e.Code != tc.wantCode || e.Message != tc.wantMsg {
			t.Fatalf("raw=%s → %+v, want %s/%s", tc.raw, e, tc.wantCode, tc.wantMsg)
		}
	}
}

// ubus 执行失败（对象缺失/超时）→ data_unavailable。
func TestExecuteRunnerError(t *testing.T) {
	m := newMgr(&capture{err: errors.New("exit status 1"), raw: "Object not found"})
	_, e := m.Execute(context.Background(), "bandix.whitelist_add", map[string]string{"mac": "aa:bb:cc:dd:ee:ff"})
	if e == nil || e.Code != envelope.CodeDataUnavailable {
		t.Fatalf("err = %+v, want data_unavailable", e)
	}
}

// 审计：成功与失败各一条（Sink 注入捕获；格式含 action 与结果）。
func TestExecuteAudit(t *testing.T) {
	var lines []string
	old := audit.Sink
	audit.Sink = func(msg string) { lines = append(lines, msg) }
	defer func() { audit.Sink = old }()

	okMgr := newMgr(&capture{raw: `{"ok":true,"api":1,"data":null}`})
	if _, e := okMgr.Execute(context.Background(), "bandix.whitelist_enabled", map[string]string{"enabled": "0"}); e != nil {
		t.Fatal(e)
	}
	failMgr := newMgr(&capture{raw: `{"ok":false,"api":1,"error":{"code":"bad_request","message":"x"}}`})
	_, _ = failMgr.Execute(context.Background(), "bandix.whitelist_enabled", map[string]string{"enabled": "0"})

	if len(lines) != 2 || !strings.Contains(lines[0], "action=bandix.whitelist_enabled") || !strings.Contains(lines[0], "ok") {
		t.Fatalf("audit lines = %v", lines)
	}
	if !strings.Contains(lines[1], "fail") || !strings.Contains(lines[1], "code=bad_request") {
		t.Fatalf("audit fail line = %q", lines[1])
	}
}

// 报文是合法 JSON（结构防呆）。
func TestPayloadIsJSON(t *testing.T) {
	c := &capture{raw: `{"ok":true}`}
	m := newMgr(c)
	_, _ = m.Execute(context.Background(), "bandix.limit_default_set",
		map[string]string{"wan_rx_rate_limit": "0", "wan_tx_rate_limit": "1099511627776"})
	var v map[string]any
	if err := json.Unmarshal([]byte(c.argv[4]), &v); err != nil {
		t.Fatalf("payload not JSON: %v (%s)", err, c.argv[4])
	}
}
