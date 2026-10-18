package uciconf

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/quartzwrt/quartz-app-api/internal/envelope"
)

// cap 记录 argv 并回放预置应答（按 method 分发）。
type cap struct {
	argv   []string
	byMeth map[string]string
	record map[string][]string // method → argv（末次调用）
	err    error
}

func (c *cap) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	c.argv = append([]string{name}, args...)
	if c.record == nil {
		c.record = map[string][]string{}
	}
	if c.err != nil {
		return []byte("boom"), c.err
	}
	method := ""
	for i, a := range args {
		if a == "call" && i+2 < len(args) {
			method = args[i+2]
		}
	}
	c.record[method] = append([]string{name}, args...)
	if v, ok := c.byMeth[method]; ok {
		return []byte(v), nil
	}
	return []byte(`{}`), nil
}

// argvFor 取某 method 的末次 argv 里最后一个参数（JSON）。
func argvFor(t *testing.T, c *cap, method string) map[string]any {
	t.Helper()
	args, ok := c.record[method]
	if !ok || len(args) == 0 {
		t.Fatalf("method %s not called", method)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(args[len(args)-1]), &m); err != nil {
		t.Fatalf("%s argv not JSON: %q", method, args[len(args)-1])
	}
	return m
}

func newMgr(c *cap, sid string) *Manager {
	return &Manager{Run: c.run, SIDFor: func(string) string { return sid }}
}

func TestReadRegisteredAndMasking(t *testing.T) {
	c := &cap{byMeth: map[string]string{
		"configs": `{"configs":["network","wireless","system"]}`,
		"get":     `{"values":{"ssid":"MyWifi","key":"supersecret","encryption":"psk2"}}`,
	}}
	m := newMgr(c, "sid-1")
	// 单选项读：ssid 明文
	c.byMeth["get"] = `{"values":{"ssid":"MyWifi"}}`
	v, e := m.Read("wireless", "@wifi-iface[0]", "ssid")
	if e != nil || v != "MyWifi" {
		t.Fatalf("read ssid: %v %v", v, e)
	}
	// 敏感项掩码
	c.byMeth["get"] = `{"values":{"key":"supersecret"}}`
	v, e = m.Read("wireless", "@wifi-iface[0]", "key")
	if e != nil || v != "__masked__" {
		t.Fatalf("read key must be masked: %v %v", v, e)
	}
	// 未注册配置 / 未注册选项
	if _, e := m.Read("firewall", "", ""); e == nil || e.Code != envelope.CodeForbiddenAction {
		t.Fatalf("unregistered config: %+v", e)
	}
	if _, e := m.Read("system", "@system[0]", "rootpw"); e == nil || e.Code != envelope.CodeForbiddenAction {
		t.Fatalf("unregistered option: %+v", e)
	}
}

// 单选项读：rpcd 带 option 时回 `{"value":[…]}`
func TestReadSingleOptionShapes(t *testing.T) {
	c := &cap{byMeth: map[string]string{"get": `{"value":["192.168.1.1/24"]}`}}
	m := newMgr(c, "sid")
	v, e := m.Read("network", "lan", "ipaddr")
	if e != nil || v != "192.168.1.1/24" {
		t.Fatalf("single value: %v %v", v, e)
	}
	c.byMeth["get"] = `{"value":[]}`
	v, e = m.Read("network", "lan", "ipaddr")
	if e != nil || v != nil {
		t.Fatalf("unset must be nil: %v %v", v, e)
	}
	c.byMeth["get"] = `{"values":{"ipaddr":"10.0.0.1"}}`
	v, e = m.Read("network", "lan", "ipaddr")
	if e != nil || v != "10.0.0.1" {
		t.Fatalf("values fallback: %v %v", v, e)
	}
}

func TestWriteValidatesAndStagesWithSession(t *testing.T) {
	c := &cap{byMeth: map[string]string{"set": `{}`, "changes": `{"changes":[["set","cfg01","hostname","qz"]]}`}}
	m := newMgr(c, "sid-abc")
	res, e := m.Write("dev1", "system", "@system[0]", "hostname", "qz-accept")
	if e != nil {
		t.Fatalf("write: %+v", e)
	}
	got := argvFor(t, c, "set")
	if got["ubus_rpc_session"] != "sid-abc" {
		t.Fatalf("session must be attached: %v", got)
	}
	if got["section"] != "@system[0]" {
		t.Fatalf("section: %v", got)
	}
	vals, _ := got["values"].(map[string]any)
	if vals["hostname"] != "qz-accept" {
		t.Fatalf("values: %v", got)
	}
	if _, has := res["changes"]; !has {
		t.Fatalf("changes must be returned: %v", res)
	}

	// 类型/枚举/字符集负例
	cases := []struct {
		name          string
		cfg, sec, opt string
		val           any
		want          string
	}{
		{"主机名非法字符", "system", "@system[0]", "hostname", "bad name;rm", envelope.CodeBadRequest},
		{"枚举外", "wireless", "@wifi-iface[0]", "encryption", "wep", envelope.CodeBadRequest},
		{"IP 非法", "network", "lan", "ipaddr", "999.1.1.1", envelope.CodeBadRequest},
		{"整数越界", "dhcp", "@dnsmasq[0]", "cachesize", float64(999999), envelope.CodeBadRequest},
		{"租期非法", "dhcp", "@dhcp[0]", "leasetime", "12y", envelope.CodeBadRequest},
		{"掩码占位符回写被拒", "wireless", "@wifi-iface[0]", "key", "__masked__", envelope.CodeBadRequest},
		{"未注册选项", "system", "@system[0]", "rootpw", "x", envelope.CodeForbiddenAction},
	}
	for _, tc := range cases {
		if _, e := m.Write("dev1", tc.cfg, tc.sec, tc.opt, tc.val); e == nil || e.Code != tc.want {
			t.Fatalf("%s: err=%+v want %s", tc.name, e, tc.want)
		}
	}
	// 合法布尔/整数归一为 uci 形态
	c2 := &cap{byMeth: map[string]string{"set": `{}`, "changes": `{}`}}
	m2 := newMgr(c2, "s")
	if _, e := m2.Write("d", "dhcp", "@dhcp[0]", "ignore", true); e != nil {
		t.Fatalf("bool write: %+v", e)
	}
	got = argvFor(t, c2, "set")
	vals, _ = got["values"].(map[string]any)
	if vals["ignore"] != float64(1) {
		t.Fatalf("bool must normalize to 1: %v", vals)
	}
}

func TestApplyConfirmAndNoSession(t *testing.T) {
	c := &cap{byMeth: map[string]string{"apply": `{}`, "confirm": `{}`}}
	m := newMgr(c, "sid-xyz")
	if _, e := m.Apply("dev1", 90, true); e != nil {
		t.Fatalf("apply: %+v", e)
	}
	got := argvFor(t, c, "apply")
	if got["rollback"] != true || got["timeout"] != float64(90) || got["ubus_rpc_session"] != "sid-xyz" {
		t.Fatalf("apply args: %v", got)
	}
	if _, e := m.Confirm("dev1"); e != nil {
		t.Fatalf("confirm: %+v", e)
	}
	// 超时越界 → 夹回默认
	if _, e := m.Apply("dev1", 100000, true); e != nil {
		t.Fatalf("apply clamp: %+v", e)
	}
	if got := argvFor(t, c, "apply"); got["timeout"] != float64(DefaultRollbackTimeout) {
		t.Fatalf("timeout clamp: %v", got)
	}

	// 无会话（未登录过 / sid 过期）→ data_unavailable 且文案含"重新登录"
	m2 := newMgr(&cap{}, "")
	if _, e := m2.Apply("dev1", 90, true); e == nil || e.Code != envelope.CodeDataUnavailable || !strings.Contains(e.Message, "重新登录") {
		t.Fatalf("no session: %+v", e)
	}
	if _, e := m2.Write("dev1", "system", "@system[0]", "hostname", "x"); e == nil || e.Code != envelope.CodeDataUnavailable {
		t.Fatalf("write without session must fail: %+v", e)
	}
}

// 静态租约：config.add 暂存新增 + config.delete 删除（section 类型/必填/白名单）。
func TestAddDeleteStaticLease(t *testing.T) {
	c := &cap{byMeth: map[string]string{
		"":       `host`,
		"add":    `{"section":"cfg0a1b"}`,
		"delete": `{}`,
	}}
	m := newMgr(c, "sid-le")
	res, e := m.Add("dev1", "dhcp", "host", map[string]any{
		"name": "printer", "mac": "02:11:22:33:44:55", "ip": "192.168.1.250",
	})
	if e != nil {
		t.Fatalf("add: %+v", e)
	}
	if res["section"] != "cfg0a1b" {
		t.Fatalf("add section: %+v", res)
	}
	got := argvFor(t, c, "add")
	if got["ubus_rpc_session"] != "sid-le" || got["type"] != "host" {
		t.Fatalf("add args: %v", got)
	}
	vals, _ := got["values"].(map[string]any)
	if vals["mac"] != "02:11:22:33:44:55" {
		t.Fatalf("add values: %v", got)
	}

	// 必填 mac / 未注册类型 / 未注册字段
	if _, e := m.Add("dev1", "dhcp", "host", map[string]any{"name": "x"}); e == nil ||
		e.Code != envelope.CodeBadRequest {
		t.Fatalf("missing mac: %+v", e)
	}
	if _, e := m.Add("dev1", "dhcp", "no-such-type", map[string]any{"mac": "02:11:22:33:44:55"}); e == nil ||
		e.Code != envelope.CodeForbiddenAction {
		t.Fatalf("bad type: %+v", e)
	}
	if _, e := m.Add("dev1", "dhcp", "host", map[string]any{"mac": "02:11:22:33:44:55", "rootpw": "x"}); e == nil ||
		e.Code != envelope.CodeForbiddenAction {
		t.Fatalf("bad option: %+v", e)
	}

	// 删除 host（sectionType 走 uci get → host）
	if _, e := m.Delete("dev1", "dhcp", "@host[0]"); e != nil {
		t.Fatalf("delete: %+v", e)
	}
	del := argvFor(t, c, "delete")
	if del["section"] != "@host[0]" || del["ubus_rpc_session"] != "sid-le" {
		t.Fatalf("delete args: %v", del)
	}

	// 非 Deletable 节（network.lan）拒绝
	c2 := &cap{byMeth: map[string]string{"": `interface`}}
	if _, e := newMgr(c2, "sid").Delete("dev1", "network", "lan"); e == nil ||
		e.Code != envelope.CodeForbiddenAction {
		t.Fatalf("non-deletable: %+v", e)
	}
}

// apply 后「立即回滚」必须走 uci rollback（revert 无法撤销已生效配置）。
func TestRollbackUsesUciRollbackWithSession(t *testing.T) {
	c := &cap{byMeth: map[string]string{"rollback": `{}`}}
	m := newMgr(c, "sid-rb")
	res, e := m.Rollback("dev1")
	if e != nil {
		t.Fatalf("rollback: %+v", e)
	}
	got := argvFor(t, c, "rollback")
	if got["ubus_rpc_session"] != "sid-rb" {
		t.Fatalf("rollback must carry session: %v", got)
	}
	if res["rolled_back"] != true {
		t.Fatalf("rollback result: %+v", res)
	}
	// 无会话 → data_unavailable（与 write/apply 同语义）
	if _, e := newMgr(&cap{}, "").Rollback("dev1"); e == nil || e.Code != envelope.CodeDataUnavailable {
		t.Fatalf("rollback without session: %+v", e)
	}
}

// uci 权限被拒（sid 已被销毁/过期）→ 映射为 data_unavailable + 重新登录文案。
func TestPermissionDeniedMapping(t *testing.T) {
	c := &cap{err: errors.New("exit 1"), byMeth: map[string]string{}}
	c.byMeth = nil
	m := &Manager{Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("Command failed: ubus call uci set {...} (Permission denied)"), errors.New("exit 1")
	}, SIDFor: func(string) string { return "stale" }}
	if _, e := m.Write("dev1", "system", "@system[0]", "hostname", "x"); e == nil || e.Code != envelope.CodeDataUnavailable {
		t.Fatalf("permission denied mapping: %+v", e)
	}
}
