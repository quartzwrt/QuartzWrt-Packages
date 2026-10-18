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

// 无参读条目（bandix.limits）：不追加 JSON argv；传参即拒。
func TestExecuteNoArgsEntry(t *testing.T) {
	c := &capture{raw: `{"ok":true,"api":1,"data":{"schedule":[],"whitelist":{"enabled":false,"macs":[]}}}`}
	m := newMgr(c)
	res, e := m.Execute(context.Background(), "bandix.limits", nil)
	if e != nil {
		t.Fatalf("bandix.limits: %v", e)
	}
	want := []string{"ubus", "call", "quartz.bandix", "limits"}
	if strings.Join(c.argv, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", c.argv, want)
	}
	if res["action"] != "bandix.limits" {
		t.Fatalf("result = %+v", res)
	}
	if _, e := m.Execute(context.Background(), "bandix.limits", map[string]string{"x": "1"}); e == nil ||
		e.Code != envelope.CodeForbiddenAction {
		t.Fatalf("extra arg should be rejected, got %+v", e)
	}
}

// luci 条目（W6.10 路由器 tab）：启动项列表/动作、时区列表。
func TestLuciEntries(t *testing.T) {
	// 启动项列表：无参调用
	c := &capture{raw: `{"uhttpd":{"index":50,"stop":null,"enabled":true}}`}
	m := newMgr(c)
	res, e := m.Execute(context.Background(), "luci.init_list", nil)
	if e != nil {
		t.Fatalf("init_list: %+v", e)
	}
	want := []string{"ubus", "call", "luci", "getInitList"}
	if strings.Join(c.argv, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", c.argv, want)
	}
	if res["action"] != "luci.init_list" {
		t.Fatalf("result = %+v", res)
	}

	// 启动项动作：平铺 {name, action}
	c = &capture{raw: `{}`}
	m = newMgr(c)
	if _, e := m.Execute(context.Background(), "luci.init_action",
		map[string]string{"name": "uhttpd", "action": "restart"}); e != nil {
		t.Fatalf("init_action: %+v", e)
	}
	got := lastArgvJSON(t, c)
	if got["name"] != "uhttpd" || got["action"] != "restart" {
		t.Fatalf("init_action payload: %v", got)
	}

	// 注入 / 路径分隔符 / 非法动作一律拒绝
	for _, bad := range []map[string]string{
		{"name": "../etc/passwd", "action": "start"},
		{"name": "uhttpd;reboot", "action": "start"},
		{"name": "uhttpd", "action": "rm"},
		{"name": "uhttpd"},
		{"action": "start"},
	} {
		if _, e := newMgr(&capture{raw: `{}`}).Execute(context.Background(), "luci.init_action", bad); e == nil ||
			e.Code != envelope.CodeBadRequest {
			t.Fatalf("bad init_action %v: %+v", bad, e)
		}
	}

	// 时区列表：无参
	c = &capture{raw: `{"Asia/Shanghai":{"tzstring":"CST-8","active":true}}`}
	m = newMgr(c)
	if _, e := m.Execute(context.Background(), "luci.timezones", nil); e != nil {
		t.Fatalf("timezones: %+v", e)
	}
	if strings.Join(c.argv, " ") != "ubus call luci getTimezones" {
		t.Fatalf("timezones argv: %v", c.argv)
	}

	// 管理权·改密：平铺 {username,password}，只允许 root
	c = &capture{raw: `{"result":true}`}
	m = newMgr(c)
	if _, e := m.Execute(context.Background(), "luci.set_password",
		map[string]string{"username": "root", "password": "s3cret with space"}); e != nil {
		t.Fatalf("set_password: %+v", e)
	}
	got = lastArgvJSON(t, c)
	if got["username"] != "root" || got["password"] != "s3cret with space" {
		t.Fatalf("set_password payload: %v", got)
	}
	for _, bad := range []map[string]string{
		{"username": "admin", "password": "secret"},
		{"username": "root", "password": ""},
		{"username": "root", "password": "a\nb"},
		{"username": "root", "password": "secret", "extra": "x"},
	} {
		if _, e := newMgr(&capture{raw: `{}`}).Execute(context.Background(), "luci.set_password", bad); e == nil {
			t.Fatalf("bad set_password %v accepted", bad)
		}
	}
}

// ---------- qmodem 蜂窝域条目（2026-10-18，W6.12） ----------

// qmodemMgr 构造带 section/port provider 的管理器。
func qmodemMgr(c *capture, sections []string, ports []string) *Manager {
	m := &Manager{Run: c.run}
	m.Sections = func() ([]string, error) { return sections, nil }
	m.Ports = func(string) ([]string, error) { return ports, nil }
	return m
}

func lastArgvJSON(t *testing.T, c *capture) map[string]any {
	t.Helper()
	if len(c.argv) == 0 {
		t.Fatal("no ubus call captured")
	}
	last := c.argv[len(c.argv)-1]
	var m map[string]any
	if err := json.Unmarshal([]byte(last), &m); err != nil {
		t.Fatalf("last argv is not JSON: %q", last)
	}
	return m
}

func TestHandlesQmodem(t *testing.T) {
	for _, id := range []string{"qmodem.dial", "qmodem.set_lockband", "qmodem.send_at",
		"qmodem.set_imei", "qmodem.at_cfg", "qmodem.tty_ports", "qmodem.set_network_prefer"} {
		if !Handles(id) {
			t.Fatalf("Handles(%q) = false", id)
		}
	}
	// 未注册的 qmodem 方法与危险/越界面（扫描、移除模组、5G LAN、写入式 set）一律拒绝
	for _, id := range []string{"qmodem.scan_all", "qmodem.remove_modem", "qmodem.set_5g_lan",
		"qmodem.set_neighborcell", "qmodem.info", "qmodem.exec", ""} {
		if Handles(id) {
			t.Fatalf("Handles(%q) = true（不应注册）", id)
		}
	}
}

func TestQmodemSectionWhitelist(t *testing.T) {
	c := &capture{raw: `{"running":"true"}`}
	// 未知 section → bad_request
	m := qmodemMgr(c, []string{"modem1"}, nil)
	if _, e := m.Execute(context.Background(), "qmodem.dial", map[string]string{"config_section": "modem9"}); e == nil || e.Code != envelope.CodeBadRequest {
		t.Fatalf("unknown section: %+v", e)
	}
	// 字符集不符 → bad_request（不触发 provider）
	if _, e := m.Execute(context.Background(), "qmodem.dial", map[string]string{"config_section": "modem1;reboot"}); e == nil || e.Code != envelope.CodeBadRequest {
		t.Fatalf("bad charset: %+v", e)
	}
	// provider 空列表（qmodem 未装/无模组）→ data_unavailable
	m2 := qmodemMgr(c, nil, nil)
	if _, e := m2.Execute(context.Background(), "qmodem.dial", map[string]string{"config_section": "modem1"}); e == nil || e.Code != envelope.CodeDataUnavailable {
		t.Fatalf("no modems: %+v", e)
	}
	// provider 缺失（nil）→ data_unavailable
	m3 := &Manager{Run: c.run}
	if _, e := m3.Execute(context.Background(), "qmodem.dial", map[string]string{"config_section": "modem1"}); e == nil || e.Code != envelope.CodeDataUnavailable {
		t.Fatalf("nil provider: %+v", e)
	}
}

func TestQmodemPayloadShapes(t *testing.T) {
	sec := map[string]string{"config_section": "modem1"}
	// shapeSectionOnly：{"config_section":"modem1"}
	c := &capture{raw: `{"running":"true"}`}
	if _, e := qmodemMgr(c, []string{"modem1"}, nil).Execute(context.Background(), "qmodem.dial", sec); e != nil {
		t.Fatalf("dial: %+v", e)
	}
	if got := lastArgvJSON(t, c); got["config_section"] != "modem1" || len(got) != 1 {
		t.Fatalf("sectionOnly payload: %v", got)
	}
	if len(c.argv) != 5 || c.argv[2] != "qmodem" || c.argv[3] != "modem_dial" {
		t.Fatalf("method argv: %v", c.argv)
	}

	// shapeFlat：{"config_section":"modem1","mode":"qmi"}
	c = &capture{raw: `{"result":"qmi"}`}
	args := map[string]string{"config_section": "modem1", "mode": "qmi"}
	if _, e := qmodemMgr(c, []string{"modem1"}, nil).Execute(context.Background(), "qmodem.set_usb_mode", args); e != nil {
		t.Fatalf("set_usb_mode: %+v", e)
	}
	if got := lastArgvJSON(t, c); got["mode"] != "qmi" {
		t.Fatalf("flat payload: %v", got)
	}

	// shapeSectionParams（对象）：params 原样嵌套
	c = &capture{raw: `{"at_cfg":{"cmd":"AT+QCAINFO","res":"OK"}}`}
	args = map[string]string{"config_section": "modem1", "params": `{"at":"AT+QCAINFO","port":"/dev/ttyUSB2"}`}
	if _, e := qmodemMgr(c, []string{"modem1"}, []string{"/dev/ttyUSB2"}).Execute(context.Background(), "qmodem.send_at", args); e != nil {
		t.Fatalf("send_at: %+v", e)
	}
	got := lastArgvJSON(t, c)
	params, ok := got["params"].(map[string]any)
	if !ok || params["at"] != "AT+QCAINFO" || params["port"] != "/dev/ttyUSB2" {
		t.Fatalf("params payload: %v", got)
	}

	// shapeSectionParams（数组）：网络偏好
	c = &capture{raw: `{"result":"ok"}`}
	args = map[string]string{"config_section": "modem1", "params": `["4G","5G"]`}
	if _, e := qmodemMgr(c, []string{"modem1"}, nil).Execute(context.Background(), "qmodem.set_network_prefer", args); e != nil {
		t.Fatalf("set_network_prefer: %+v", e)
	}
	got = lastArgvJSON(t, c)
	arr, ok := got["params"].([]any)
	if !ok || len(arr) != 2 || arr[0] != "4G" || arr[1] != "5G" {
		t.Fatalf("array payload: %v", got)
	}

	// shapeNone：不带 JSON argv
	c = &capture{raw: `{"ports":["/dev/ttyUSB2"]}`}
	if _, e := qmodemMgr(c, []string{"modem1"}, nil).Execute(context.Background(), "qmodem.tty_ports", map[string]string{}); e != nil {
		t.Fatalf("tty_ports: %+v", e)
	}
	if len(c.argv) != 4 || c.argv[2] != "qmodem" || c.argv[3] != "get_tty_ports" {
		t.Fatalf("shapeNone must omit JSON argv: %v", c.argv)
	}
}

func TestQmodemArgValidation(t *testing.T) {
	sec := "modem1"
	m := qmodemMgr(&capture{raw: `{"ok":true}`}, []string{sec}, []string{"/dev/ttyUSB2"})
	cases := []struct {
		name   string
		id     string
		args   map[string]string
		wantCD string
	}{
		{"at 缺 AT 前缀", "qmodem.send_at", map[string]string{"config_section": sec, "params": `{"at":"QCAINFO","port":"/dev/ttyUSB2"}`}, envelope.CodeBadRequest},
		{"at 注入分号", "qmodem.send_at", map[string]string{"config_section": sec, "params": `{"at":"ATI;reboot","port":"/dev/ttyUSB2"}`}, envelope.CodeBadRequest},
		{"at 反引号注入", "qmodem.send_at", map[string]string{"config_section": sec, "params": "{\"at\":\"ATI`id`\",\"port\":\"/dev/ttyUSB2\"}"}, envelope.CodeBadRequest},
		{"at 过长", "qmodem.send_at", map[string]string{"config_section": sec, "params": `{"at":"AT` + strings.Repeat("A", 260) + `","port":"/dev/ttyUSB2"}`}, envelope.CodeBadRequest},
		{"port 越界路径", "qmodem.send_at", map[string]string{"config_section": sec, "params": `{"at":"ATI","port":"/etc/shadow"}`}, envelope.CodeBadRequest},
		{"port 不在该模组", "qmodem.send_at", map[string]string{"config_section": sec, "params": `{"at":"ATI","port":"/dev/ttyUSB9"}`}, envelope.CodeBadRequest},
		{"params 未知键", "qmodem.send_at", map[string]string{"config_section": sec, "params": `{"at":"ATI","port":"/dev/ttyUSB2","sudo":1}`}, envelope.CodeForbiddenAction},
		{"params 非 JSON", "qmodem.send_at", map[string]string{"config_section": sec, "params": `ATI`}, envelope.CodeBadRequest},
		{"imei 位数不符", "qmodem.set_imei", map[string]string{"config_section": sec, "imei": "12345"}, envelope.CodeBadRequest},
		{"imei 含字母", "qmodem.set_imei", map[string]string{"config_section": sec, "imei": "12345678901234A"}, envelope.CodeBadRequest},
		{"mode 非枚举", "qmodem.set_usb_mode", map[string]string{"config_section": sec, "mode": "fiber"}, envelope.CodeBadRequest},
		{"storage 非枚举", "qmodem.set_sms_storage", map[string]string{"config_section": sec, "storage": "XX"}, envelope.CodeBadRequest},
		{"reset_type 非枚举", "qmodem.set_traffic_reset_schedule", map[string]string{"config_section": sec, "enabled": "1", "reset_type": "weekly", "hour": "0", "day": "1"}, envelope.CodeBadRequest},
		{"hour 越界", "qmodem.set_traffic_reset_schedule", map[string]string{"config_section": sec, "enabled": "1", "reset_type": "daily", "hour": "24", "day": "1"}, envelope.CodeBadRequest},
		{"day 越界", "qmodem.set_traffic_reset_schedule", map[string]string{"config_section": sec, "enabled": "1", "reset_type": "monthly", "hour": "0", "day": "29"}, envelope.CodeBadRequest},
		{"index 列表非法", "qmodem.delete_sms", map[string]string{"config_section": sec, "index": "1 2;3"}, envelope.CodeBadRequest},
		{"sms 号码非法", "qmodem.send_sms", map[string]string{"config_section": sec, "params": `{"phone_number":"10086;reboot","message_content":"hi"}`}, envelope.CodeBadRequest},
		{"sms 内容为空", "qmodem.send_sms", map[string]string{"config_section": sec, "params": `{"phone_number":"10086","message_content":""}`}, envelope.CodeBadRequest},
		{"lockband 键未知", "qmodem.set_lockband", map[string]string{"config_section": sec, "params": `{"band":"1"}`}, envelope.CodeForbiddenAction},
		{"lockband class 非枚举", "qmodem.set_lockband", map[string]string{"config_section": sec, "params": `{"band_class":"WCDMA","lock_band":"1"}`}, envelope.CodeBadRequest},
		{"prefer 元素非枚举", "qmodem.set_network_prefer", map[string]string{"config_section": sec, "params": `["2G"]`}, envelope.CodeBadRequest},
		{"slot 非枚举", "qmodem.set_sim_slot", map[string]string{"config_section": sec, "slot": "3"}, envelope.CodeBadRequest},
		{"reboot method 非枚举", "qmodem.reboot", map[string]string{"config_section": sec, "params": `{"method":"nuke"}`}, envelope.CodeBadRequest},
		{"未知参数键", "qmodem.dial", map[string]string{"config_section": sec, "extra": "1"}, envelope.CodeForbiddenAction},
	}
	for _, tc := range cases {
		if _, e := m.Execute(context.Background(), tc.id, tc.args); e == nil || e.Code != tc.wantCD {
			t.Fatalf("%s: err = %+v, want %s", tc.name, e, tc.wantCD)
		}
	}
	// 正例：合法 AT / IMEI / lockband / prefer / 时间表
	ok := []struct {
		name string
		id   string
		args map[string]string
	}{
		{"at 合法", "qmodem.send_at", map[string]string{"config_section": sec, "params": `{"at":"AT+QENG=\"servingcell\"","port":"/dev/ttyUSB2"}`}},
		{"imei 合法", "qmodem.set_imei", map[string]string{"config_section": sec, "imei": "861234567890123"}},
		{"lockband 合法", "qmodem.set_lockband", map[string]string{"config_section": sec, "params": `{"band_class":"NR","lock_band":"41,78,79"}`}},
		{"prefer 合法", "qmodem.set_network_prefer", map[string]string{"config_section": sec, "params": `["4G","5G"]`}},
		{"时间表合法", "qmodem.set_traffic_reset_schedule", map[string]string{"config_section": sec, "enabled": "1", "reset_type": "monthly", "hour": "3", "day": "1"}},
	}
	for _, tc := range ok {
		if _, e := m.Execute(context.Background(), tc.id, tc.args); e != nil {
			t.Fatalf("%s: unexpected %+v", tc.name, e)
		}
	}
}

// 端口 provider 不可用（qmodem 无 get_tty_ports 或调用失败）→ 回退字符集校验。
func TestQmodemPortProviderFallback(t *testing.T) {
	c := &capture{raw: `{"ok":true}`}
	m := &Manager{Run: c.run}
	m.Sections = func() ([]string, error) { return []string{"modem1"}, nil }
	m.Ports = func(string) ([]string, error) { return nil, errors.New("unavailable") }
	args := map[string]string{"config_section": "modem1", "params": `{"at":"ATI","port":"/dev/ttyUSB9"}`}
	if _, e := m.Execute(context.Background(), "qmodem.send_at", args); e != nil {
		t.Fatalf("fallback should accept charset-valid port: %+v", e)
	}
}

// qmodem 裸对象应答（无 ok 字段）→ 成功且整对象作 data；{"error":…} → action_failed。
func TestQmodemBareObjectResponse(t *testing.T) {
	sec := map[string]string{"config_section": "modem1"}
	c := &capture{raw: `{"running":"true"}`}
	res, e := qmodemMgr(c, []string{"modem1"}, nil).Execute(context.Background(), "qmodem.dial", sec)
	if e != nil {
		t.Fatalf("bare object must succeed: %+v", e)
	}
	r, _ := res["result"].(map[string]any)
	d, _ := r["data"].(map[string]any)
	if d["running"] != "true" {
		t.Fatalf("bare data: %+v", res)
	}

	c = &capture{raw: `{"error":"unknown modem section"}`}
	_, e = qmodemMgr(c, []string{"modem1"}, nil).Execute(context.Background(), "qmodem.dial", sec)
	if e == nil || e.Code != envelope.CodeActionFailed || !strings.Contains(e.Message, "unknown modem section") {
		t.Fatalf("bare error: %+v", e)
	}
}

// 桥接旧形态不得回归：bandix 条目仍走 data 包裹与信封解析。
func TestBandixEntriesUnchanged(t *testing.T) {
	c := &capture{raw: `{"ok":true,"data":{"limits":1}}`}
	args := map[string]string{"wan_rx_rate_limit": "1024", "wan_tx_rate_limit": "0"}
	if _, e := qmodemMgr(c, []string{"modem1"}, nil).Execute(context.Background(), "bandix.limit_default_set", args); e != nil {
		t.Fatalf("bandix: %+v", e)
	}
	got := lastArgvJSON(t, c)
	data, ok := got["data"].(map[string]any)
	if !ok || data["wan_rx_rate_limit"] == nil {
		t.Fatalf("bandix data wrap: %v", got)
	}
	c2 := &capture{raw: `{"ok":true,"data":{"queries":1}}`}
	if _, e := qmodemMgr(c2, nil, nil).Execute(context.Background(), "bandix.limits", map[string]string{}); e != nil {
		t.Fatalf("bandix.limits: %+v", e)
	}
	if len(c2.argv) != 4 || c2.argv[2] != "quartz.bandix" || c2.argv[3] != "limits" {
		t.Fatalf("bandix.limits must omit JSON argv: %v", c2.argv)
	}
}

// at_cfg 的可选 lang（App 界面语言 → 预置命令语言）
func TestQmodemAtCfgLangOptional(t *testing.T) {
	m := qmodemMgr(&capture{raw: `{"at_cfg":{"cmds":[]}}`}, []string{"modem1"}, nil)
	// 不传 lang：合法（上游回退路由器语言）
	c := &capture{raw: `{"at_cfg":{"cmds":[]}}`}
	m2 := qmodemMgr(c, []string{"modem1"}, nil)
	if _, e := m2.Execute(context.Background(), "qmodem.at_cfg", map[string]string{"config_section": "modem1"}); e != nil {
		t.Fatalf("without lang: %+v", e)
	}
	if got := lastArgvJSON(t, c); len(got) != 1 {
		t.Fatalf("lang must be omitted when absent: %v", got)
	}
	// 传 lang=en：透传到上游
	c = &capture{raw: `{"at_cfg":{"cmds":[]}}`}
	m3 := qmodemMgr(c, []string{"modem1"}, nil)
	if _, e := m3.Execute(context.Background(), "qmodem.at_cfg", map[string]string{"config_section": "modem1", "lang": "en"}); e != nil {
		t.Fatalf("with lang: %+v", e)
	}
	if got := lastArgvJSON(t, c); got["lang"] != "en" {
		t.Fatalf("lang not forwarded: %v", got)
	}
	// 非法语言值
	if _, e := m.Execute(context.Background(), "qmodem.at_cfg", map[string]string{"config_section": "modem1", "lang": "de"}); e == nil || e.Code != envelope.CodeBadRequest {
		t.Fatalf("bad lang: %+v", e)
	}
}

// 短信条目走独立对象 qmodem.sms，且模组参数名是 modem_id（不是 config_section）。
func TestQmodemSmsObjectEntries(t *testing.T) {
	section := []string{"modem1"}
	// list：读条目，limit/offset 可选
	c := &capture{raw: `{"status":"ok","messages":[{"id":1,"index":3,"type":"received","sender":"10086","content":"hi","is_read":false}]}`}
	m := qmodemMgr(c, section, nil)
	res, e := m.Execute(context.Background(), "qmodem.sms_list",
		map[string]string{"config_section": "modem1", "limit": "50"})
	if e != nil {
		t.Fatalf("sms_list: %+v", e)
	}
	got := lastArgvJSON(t, c)
	if got["modem_id"] != "modem1" {
		t.Fatalf("section key must be modem_id: %v", got)
	}
	if _, hasConfig := got["config_section"]; hasConfig {
		t.Fatalf("config_section must not leak upstream: %v", got)
	}
	if got["limit"] == nil {
		t.Fatalf("limit must be forwarded: %v", got)
	}
	if c.argv[2] != "qmodem.sms" || c.argv[3] != "list" {
		t.Fatalf("target: %v", c.argv)
	}
	r, _ := res["result"].(map[string]any)
	d, _ := r["data"].(map[string]any)
	if d["status"] != "ok" {
		t.Fatalf("data: %+v", res)
	}
	// 可选参数缺省时不出现
	c2 := &capture{raw: `{"status":"ok"}`}
	if _, e := qmodemMgr(c2, section, nil).Execute(context.Background(), "qmodem.sms_list", map[string]string{"config_section": "modem1"}); e != nil {
		t.Fatalf("sms_list without paging: %+v", e)
	}
	if got := lastArgvJSON(t, c2); len(got) != 1 {
		t.Fatalf("optional paging must be omitted: %v", got)
	}

	// send：recipient + content（冲突字符集拒绝）
	c3 := &capture{raw: `{"status":"ok"}`}
	m3 := qmodemMgr(c3, section, nil)
	args := map[string]string{"config_section": "modem1", "recipient": "+8613800138000", "content": "hello"}
	if _, e := m3.Execute(context.Background(), "qmodem.sms_send", args); e != nil {
		t.Fatalf("sms_send: %+v", e)
	}
	got = lastArgvJSON(t, c3)
	if got["modem_id"] != "modem1" || got["recipient"] != "+8613800138000" || got["content"] != "hello" {
		t.Fatalf("send payload: %v", got)
	}
	bad := map[string]string{"config_section": "modem1", "recipient": "10086;reboot", "content": "x"}
	if _, e := m3.Execute(context.Background(), "qmodem.sms_send", bad); e == nil || e.Code != envelope.CodeBadRequest {
		t.Fatalf("bad recipient: %+v", e)
	}
	// delete：id + index 必填
	if _, e := m3.Execute(context.Background(), "qmodem.sms_delete", map[string]string{"config_section": "modem1", "id": "1"}); e == nil || e.Code != envelope.CodeBadRequest {
		t.Fatalf("delete missing index: %+v", e)
	}
	// storage_set：mem1 必填、mem2/3 可选
	c4 := &capture{raw: `{"status":"ok"}`}
	m4 := qmodemMgr(c4, section, nil)
	if _, e := m4.Execute(context.Background(), "qmodem.sms_storage_set", map[string]string{"config_section": "modem1", "mem1": "SM"}); e != nil {
		t.Fatalf("storage_set: %+v", e)
	}
	if got := lastArgvJSON(t, c4); got["mem1"] != "SM" || got["mem2"] != nil {
		t.Fatalf("storage payload: %v", got)
	}
	if _, e := m4.Execute(context.Background(), "qmodem.sms_storage_set", map[string]string{"config_section": "modem1", "mem1": "XX"}); e == nil || e.Code != envelope.CodeBadRequest {
		t.Fatalf("bad storage enum: %+v", e)
	}
	// 旧低层条目已移除（避免裸露 tom_modem 原始输出）
	if Handles("qmodem.sms") {
		t.Fatal("legacy qmodem.sms (get_sms) entry must be gone")
	}
}
