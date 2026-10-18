// Package uciconf 配置面（WP-6 W6.10「路由器 tab」；DESIGN §5.2 新增方法族）。
//
// 纪律（与 actions / ubusfwd 同款）：**无任意透传**——目标配置、section、option
// 全部**逐项注册**（`registry.go`，按「节」完整声明字段类型），值只做类型/枚举/
// 范围/字符集校验，绝不拼接 shell；执行 = `ubus call uci …`（JSON 单个 argv）。
//
// **会话模型（VM 实测，2026-10-19）**：rpcd 的 uci 写入与 apply/confirm 必须携带
// **同一 `ubus_rpc_session`**——pending 改动与回滚快照都按会话隔离（`/tmp/.uci/sessions/<sid>`）；
// 且 `session create` 得到的是**空 ACL** 会话（写 uci 被拒），只有 `session login`
// （管理员凭据）的会话有全量 ACL。app-api 是 rpcd exec 插件、每次调用都是新进程
// → 会话 sid 在 login 时开启并**持久化到设备记录**（`store.Device.ConfigSID`）。
//
// **安全网（与网页端同语义）**：`config.apply{rollback:true,timeout:90}` 提交后若
// 未在窗口内 `config.confirm`，rpcd 自动回滚（VM 实测 5s 窗口生效）。
package uciconf

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/quartzwrt/quartz-app-api/internal/audit"
	"github.com/quartzwrt/quartz-app-api/internal/envelope"
)

// Runner 执行器（注入测试用）。
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Manager 配置面执行器。
type Manager struct {
	Run Runner
	// SIDFor 取设备的配置会话 sid（缺失/过期 → re_login_required）。
	SIDFor func(deviceID string) string
	// Now 供测试注入。
	Now func() time.Time
}

// New 构造（生产：exec + 由 main 注入的 SIDFor）。
func New(sidFor func(string) string) *Manager {
	return &Manager{Run: execRun, SIDFor: sidFor, Now: time.Now}
}

// DefaultRollbackTimeout 默认回滚窗口（秒；与网页端一致）。
const DefaultRollbackTimeout = 90

// 会话不可用（未登录过配置面 / sid 过期）。
const errNoSession = "配置会话不可用，请重新登录路由器以启用配置功能"

// ------------- 读 -------------

// Read 读一个配置（`config` 全量，或指定 section / option）。
//
// 读面白名单 = 注册表内的配置；敏感值（wifi key / pppoe 口令）返回掩码占位
// （`__masked__`），避免明文离开路由器。
func (m *Manager) Read(config, section, option string) (any, *envelope.Error) {
	spec, ok := lookupConfig(config)
	if !ok {
		return nil, envelope.ED(envelope.CodeForbiddenAction, "config not registered", config)
	}
	if section == "" {
		// 全量读：走 uci export? 不 —— 逐节读，避免导出未注册选项
		vals, ee := m.readAll(spec)
		if ee != nil {
			return nil, ee
		}
		return vals, nil
	}
	if option == "" {
		vals, ee := m.readSection(spec, section)
		if ee != nil {
			return nil, ee
		}
		return vals, nil
	}
	opt, ok := lookupOption(spec, section, option)
	if !ok {
		return nil, envelope.ED(envelope.CodeForbiddenAction, "option not registered", section+"."+option)
	}
	raw, ee := m.uciCall("get", map[string]any{"config": config, "section": section, "option": option})
	if ee != nil {
		return nil, ee
	}
	// 形态（VM 实测）：带 `option` 时 rpcd 回 `{"value":["<v>"]}`（**数组包装**，
	// 单值也是数组；未设置为空数组）；不带 option 时才是 `{"values":{…}}`。
	var got struct {
		Values map[string]any  `json:"values"`
		Value  json.RawMessage `json:"value"`
	}
	_ = json.Unmarshal(raw, &got)
	if len(got.Value) > 0 {
		var arr []any
		if json.Unmarshal(got.Value, &arr) == nil {
			if len(arr) == 0 {
				return nil, nil // 未设置
			}
			if len(arr) == 1 {
				return maskIfSensitive(opt, arr[0]), nil
			}
			return maskIfSensitive(opt, arr), nil
		}
		var single any
		if json.Unmarshal(got.Value, &single) == nil {
			return maskIfSensitive(opt, single), nil
		}
	}
	v, has := got.Values[option]
	if !has {
		return nil, nil // 未设置 → 空（App 显示默认值/占位）
	}
	return maskIfSensitive(opt, v), nil
}

// readAll 全量读：仅返回注册表内的 section × option（未注册项不离开路由器）。
func (m *Manager) readAll(spec configSpec) (map[string]map[string]any, *envelope.Error) {
	out := map[string]map[string]any{}
	raw, ee := m.uciCall("configs", nil)
	if ee != nil {
		return nil, ee
	}
	var cfgList struct {
		Configs []string `json:"configs"`
	}
	_ = json.Unmarshal(raw, &cfgList)
	found := false
	for _, c := range cfgList.Configs {
		if c == spec.Name {
			found = true
		}
	}
	if !found {
		return map[string]map[string]any{}, nil
	}
	secs, ee := m.sectionNames(spec)
	if ee != nil {
		return nil, ee
	}
	for _, s := range secs {
		vals, ee := m.readSection(spec, s)
		if ee != nil {
			return nil, ee
		}
		out[s] = vals
	}
	return out, nil
}

// sectionNames 列出该配置下的 section 名（`uci -q show <config>` 解析，控制面只读）。
func (m *Manager) sectionNames(spec configSpec) ([]string, *envelope.Error) {
	raw, err := m.Run(context.Background(), "uci", "-q", "show", spec.Name)
	if err != nil {
		// 配置不存在（如未装对应包）→ 视为空
		return nil, nil
	}
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		parts := strings.Split(key, ".")
		if len(parts) < 2 || parts[0] != spec.Name {
			continue
		}
		name := parts[1]
		if !seen[name] {
			if _, ok := lookupSection(spec, name, strings.Trim(val, `'"`)); ok {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out, nil
}

// readSection 读一节内的注册选项（未注册项不返回）。
func (m *Manager) readSection(spec configSpec, section string) (map[string]any, *envelope.Error) {
	typ, ee := m.sectionType(spec, section)
	if ee != nil {
		return nil, ee
	}
	sec, ok := lookupSection(spec, section, typ)
	if !ok {
		return nil, envelope.ED(envelope.CodeForbiddenAction, "section not registered", section)
	}
	raw, ee := m.uciCall("get", map[string]any{"config": spec.Name, "section": section})
	if ee != nil {
		return nil, ee
	}
	var got struct {
		Values map[string]any `json:"values"`
	}
	_ = json.Unmarshal(raw, &got)
	out := map[string]any{}
	for name, o := range sec.Options {
		if v, has := got.Values[name]; has {
			out[name] = maskIfSensitive(o, v)
		}
	}
	return out, nil
}

// sectionType 取 section 的 uci 类型（`uci get <config>.<section>` 返回类型名）。
func (m *Manager) sectionType(spec configSpec, section string) (string, *envelope.Error) {
	raw, err := m.Run(context.Background(), "uci", "-q", "get", spec.Name+"."+section)
	if err != nil {
		return "", nil // 不存在 → 由 lookupSection 决定（通常拒绝）
	}
	return strings.Trim(strings.TrimSpace(string(raw)), `'"`), nil
}

// ------------- 写（暂存） -------------

// Write 写入一个选项（**会话暂存**，需 config.apply 才落盘）。
func (m *Manager) Write(deviceID, config, section, option string, value any) (map[string]any, *envelope.Error) {
	sid, ee := m.requireSession(deviceID)
	if ee != nil {
		return nil, ee
	}
	spec, ok := lookupConfig(config)
	if !ok {
		return nil, envelope.ED(envelope.CodeForbiddenAction, "config not registered", config)
	}
	opt, ok := lookupOption(spec, section, option)
	if !ok {
		return nil, envelope.ED(envelope.CodeForbiddenAction, "option not registered", section+"."+option)
	}
	v, ee := validateValue(opt, value)
	if ee != nil {
		return nil, ee
	}
	if _, ee := m.uciCallSID(sid, "set", map[string]any{
		"config": config, "section": section, "values": map[string]any{option: v},
	}); ee != nil {
		return nil, ee
	}
	audit.Logf("config write cfg=%s.%s.%s device=%s", config, section, option, deviceID)
	return m.Changes(deviceID, config)
}

// Changes 查看某配置的待应用改动（会话暂存）。
func (m *Manager) Changes(deviceID, config string) (map[string]any, *envelope.Error) {
	sid, ee := m.requireSession(deviceID)
	if ee != nil {
		return nil, ee
	}
	raw, ee := m.uciCallSID(sid, "changes", map[string]any{"config": config})
	if ee != nil {
		return nil, ee
	}
	var v map[string]any
	_ = json.Unmarshal(raw, &v)
	if v == nil {
		v = map[string]any{}
	}
	return v, nil
}

// Revert 丢弃某配置的待应用改动。
func (m *Manager) Revert(deviceID, config string) (map[string]any, *envelope.Error) {
	sid, ee := m.requireSession(deviceID)
	if ee != nil {
		return nil, ee
	}
	if _, ee := m.uciCallSID(sid, "revert", map[string]any{"config": config}); ee != nil {
		return nil, ee
	}
	audit.Logf("config revert cfg=%s device=%s", config, deviceID)
	return map[string]any{"reverted": config}, nil
}

// ------------- 应用 / 确认 / 回滚 -------------

// Apply 提交待应用改动；rollback=true 时武装回滚窗口（超时未 confirm 自动回滚）。
func (m *Manager) Apply(deviceID string, timeout int, rollback bool) (map[string]any, *envelope.Error) {
	sid, ee := m.requireSession(deviceID)
	if ee != nil {
		return nil, ee
	}
	if timeout <= 0 || timeout > 600 {
		timeout = DefaultRollbackTimeout
	}
	args := map[string]any{"rollback": rollback, "timeout": timeout}
	if _, ee := m.uciCallSID(sid, "apply", args); ee != nil {
		return nil, ee
	}
	audit.Logf("config apply device=%s rollback=%v timeout=%d", deviceID, rollback, timeout)
	return map[string]any{"applied": true, "rollback": rollback, "timeout": timeout}, nil
}

// Confirm 确认改动（取消回滚窗口）。
func (m *Manager) Confirm(deviceID string) (map[string]any, *envelope.Error) {
	sid, ee := m.requireSession(deviceID)
	if ee != nil {
		return nil, ee
	}
	if _, ee := m.uciCallSID(sid, "confirm", nil); ee != nil {
		return nil, ee
	}
	audit.Logf("config confirm device=%s", deviceID)
	return map[string]any{"confirmed": true}, nil
}

// Rollback 立即回滚已 apply 但未 confirm 的改动（App 的「立即回滚」按钮）。
//
// **不能复用 Revert**：VM 实测 apply 后调用 `uci revert` 只丢弃（已空的）暂存改动，
// 已生效配置原样保留、且会解除回滚计时器——配置就「漂」在那里。立即撤销必须走
// rpcd uci 的 `rollback` 方法（同一会话）。
func (m *Manager) Rollback(deviceID string) (map[string]any, *envelope.Error) {
	sid, ee := m.requireSession(deviceID)
	if ee != nil {
		return nil, ee
	}
	if _, ee := m.uciCallSID(sid, "rollback", nil); ee != nil {
		return nil, ee
	}
	audit.Logf("config rollback device=%s", deviceID)
	return map[string]any{"rolled_back": true}, nil
}

// ------------- 内部 -------------

func (m *Manager) requireSession(deviceID string) (string, *envelope.Error) {
	if m.SIDFor == nil {
		return "", envelope.E(envelope.CodeDataUnavailable, errNoSession)
	}
	sid := m.SIDFor(deviceID)
	if sid == "" {
		return "", envelope.E(envelope.CodeDataUnavailable, errNoSession)
	}
	return sid, nil
}

// uciCall 无会话读（读面走配置上下文，不需 delta）。
func (m *Manager) uciCall(method string, args map[string]any) ([]byte, *envelope.Error) {
	return m.runUCI(method, args)
}

// uciCallSID 带会话写（set/revert/apply/confirm/rollback 需同一 sid）。
func (m *Manager) uciCallSID(sid, method string, args map[string]any) ([]byte, *envelope.Error) {
	if args == nil {
		args = map[string]any{}
	}
	args["ubus_rpc_session"] = sid
	return m.runUCI(method, args)
}

func (m *Manager) runUCI(method string, args map[string]any) ([]byte, *envelope.Error) {
	if args == nil {
		args = map[string]any{} // 无参方法也要传 `{}`——`null` 会被 rpcd 判为解析失败
	}
	body, err := json.Marshal(args)
	if err != nil {
		return nil, envelope.E(envelope.CodeInternal, "marshal failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := m.Run(ctx, "ubus", "call", "uci", method, string(body))
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if strings.Contains(msg, "Permission denied") {
			return nil, envelope.E(envelope.CodeDataUnavailable, errNoSession)
		}
		if strings.Contains(msg, "Not found") {
			return nil, envelope.ED(envelope.CodeBadRequest, "uci target not found", msg)
		}
		return nil, envelope.ED(envelope.CodeActionFailed, "uci call failed", truncate(msg, 120))
	}
	return out, nil
}

func maskIfSensitive(o optionSpec, v any) any {
	if !o.Sensitive {
		return v
	}
	switch t := v.(type) {
	case string:
		if t != "" {
			return "__masked__"
		}
	case []any:
		if len(t) > 0 {
			return []any{"__masked__"}
		}
	}
	return v
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func execRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	p, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("executable not found: %s", name)
	}
	c := exec.CommandContext(ctx, p, args...)
	return c.CombinedOutput()
}
