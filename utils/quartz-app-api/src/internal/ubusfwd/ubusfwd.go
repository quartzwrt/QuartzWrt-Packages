// Package ubusfwd ubus 转发项机制（WP-5；DESIGN §5「ubus 转发项」落地，2026-10-17）。
//
// 纪律（与 actions / pkgs 包同款）：**无任意透传**——目标 (object, method) 与
// 参数模式**逐项显式注册**（表驱动），参数只做结构 + 字符集/数值域校验，绝不
// 拼接 shell；执行 = `ubus call <object> <method> <json>`（JSON 作为单个 argv）。
//
// 首批条目 = bandix bridge（WP-8 契约 `quartz.bandix`，bridge DESIGN §3.3 写面
// 白名单子集；App L2 富化行的动作条目）：
//
//	bandix.limit_default_set   → quartz.bandix limit_default_set（数值字段收 data 对象）
//	bandix.whitelist_add       → …（mac）
//	bandix.whitelist_delete    → …（mac）
//	bandix.whitelist_enabled   → …（enabled 0/1，data 对象）
//	bandix.hostname_set        → …（mac + hostname；纯字符串方法保持扁平）
//
// **入参形态（Wrap）**：bridge 的 rpcd ucode 策略要求含数值字段的方法把参数收进
// `data` 对象（WP-8 坑位 #61：blobmsg 传输层按数值宽度选 int32/int64，单一类型
// 提示必拒一半调用）；纯字符串方法保持扁平。本表逐项声明，转发时按声明重建。
//
// **读面不经本机制**：App 读 bandix 状态走 daemon 快照（AGGREGATE-API §3.7），
// 不经 app-api 转发（少一跳、且与 Dashboard 同字节）。
//
// **扩展**：新条目 = registry 加一行 + 测试；新对象（如 ddns bridge）同样只加行
// （ID 前缀路由由 Handles 判定），机制不变。
package ubusfwd

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/quartzwrt/quartz-app-api/internal/audit"
	"github.com/quartzwrt/quartz-app-api/internal/envelope"
)

// Timeout 单次转发上限（bridge 端为本地 http 127.0.0.1 调用，5s 富余）。
const Timeout = 5 * time.Second

// maxRate bridge 契约数值上限（字节数/Byte/s，0…2^40 = 1 TiB）。
const maxRate = uint64(1) << 40

// Runner 执行器（注入测试用）。返回 stdout（ubus call 的 JSON 输出）与退出错误。
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

var (
	macRe      = regexp.MustCompile(`^[0-9a-fA-F]{2}(:[0-9a-fA-F]{2}){5}$`)
	hostnameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)
	digitsRe   = regexp.MustCompile(`^[0-9]{1,20}$`)
)

// 参数类型。
type kind int

const (
	kindMAC     kind = iota // MAC（归一化为小写）
	kindUint                // 十进制整数 + 域
	kindHost                // 主机名（非空）
	kindHostOpt             // 主机名（允许空串 = 清除绑定）
)

type field struct {
	kind kind
	min  uint64
	max  uint64
}

// entry 一条转发项。
type entry struct {
	id     string // action id（App 侧）
	object string // ubus 对象（固定）
	method string // 上游方法（固定）
	wrap   string // ""=扁平参数；"data"=参数收进 data 对象（bridge 数值方法形态）
	fields map[string]field
}

var registry = []entry{
	{
		id: "bandix.limit_default_set", object: "quartz.bandix", method: "limit_default_set", wrap: "data",
		fields: map[string]field{
			"wan_rx_rate_limit": {kind: kindUint, max: maxRate},
			"wan_tx_rate_limit": {kind: kindUint, max: maxRate},
		},
	},
	{
		id: "bandix.whitelist_add", object: "quartz.bandix", method: "whitelist_add",
		fields: map[string]field{"mac": {kind: kindMAC}},
	},
	{
		id: "bandix.whitelist_delete", object: "quartz.bandix", method: "whitelist_delete",
		fields: map[string]field{"mac": {kind: kindMAC}},
	},
	{
		id: "bandix.whitelist_enabled", object: "quartz.bandix", method: "whitelist_enabled", wrap: "data",
		fields: map[string]field{"enabled": {kind: kindUint, max: 1}},
	},
	{
		id: "bandix.hostname_set", object: "quartz.bandix", method: "hostname_set",
		fields: map[string]field{"mac": {kind: kindMAC}, "hostname": {kind: kindHostOpt}},
	},
	{
		// 唯一读条目：限速/白名单/配额聚合（无参）——App 的「设置类对话框」取当前值
		// （该状态不在 daemon 快照内）；状态/流量类读面一律走快照，见包注释。
		id: "bandix.limits", object: "quartz.bandix", method: "limits",
		fields: map[string]field{},
	},
}

// Handles 判定 action id 是否由本机制承载（main.go 前缀路由用）。
func Handles(id string) bool {
	for _, e := range registry {
		if e.id == id {
			return true
		}
	}
	return false
}

// Manager 转发执行器。
type Manager struct {
	Run Runner // 默认 exec 直执行
}

// New 构造。
func New() *Manager { return &Manager{Run: execRun} }

// Execute 校验并转发一条动作；一切尝试（含失败）落 logd 审计。
func (m *Manager) Execute(ctx context.Context, id string, args map[string]string) (map[string]any, *envelope.Error) {
	var ent *entry
	for i := range registry {
		if registry[i].id == id {
			ent = &registry[i]
			break
		}
	}
	if ent == nil {
		return nil, envelope.ED(envelope.CodeForbiddenAction, "unknown action", id)
	}

	out, ee := validate(ent, args)
	if ee != nil {
		audit.Logf("fwd action=%s fail reason=invalid", id)
		return nil, ee
	}

	// 有参条目：按声明重建 JSON（数值收 data 对象或扁平）；无参条目：不追加 JSON argv
	// （ubus call 对无参方法多传空对象虽通常可容忍，但省略最稳）。
	argv := []string{"call", ent.object, ent.method}
	if len(ent.fields) > 0 {
		payload := map[string]any{}
		if ent.wrap == "data" {
			payload["data"] = out
		} else {
			payload = out
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, envelope.E(envelope.CodeInternal, "marshal failed")
		}
		argv = append(argv, string(body))
	}

	start := time.Now()
	cctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	raw, err := m.Run(cctx, "ubus", argv...)
	dur := time.Since(start)
	if err != nil {
		audit.Logf("fwd action=%s target=%s.%s fail dur_ms=%d reason=exec", id, ent.object, ent.method, dur.Milliseconds())
		return nil, envelope.ED(envelope.CodeDataUnavailable, "bridge unavailable", strings.TrimSpace(string(raw)))
	}

	var env struct {
		OK    bool            `json:"ok"`
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		audit.Logf("fwd action=%s fail dur_ms=%d reason=parse", id, dur.Milliseconds())
		return nil, envelope.ED(envelope.CodeActionFailed, "bad upstream response", truncate(string(raw), 200))
	}
	if !env.OK {
		msg, code := "upstream error", envelope.CodeActionFailed
		if env.Error != nil {
			if env.Error.Message != "" {
				msg = env.Error.Message
			}
			switch env.Error.Code {
			case "bad_request":
				code = envelope.CodeBadRequest
			case "data_unavailable":
				code = envelope.CodeDataUnavailable
			}
		}
		audit.Logf("fwd action=%s target=%s.%s fail dur_ms=%d code=%s", id, ent.object, ent.method, dur.Milliseconds(), code)
		return nil, envelope.ED(code, msg, "")
	}

	var data any
	if len(env.Data) > 0 {
		_ = json.Unmarshal(env.Data, &data)
	}
	audit.Logf("fwd action=%s target=%s.%s ok dur_ms=%d", id, ent.object, ent.method, dur.Milliseconds())
	return map[string]any{
		"action": id,
		"result": map[string]any{"data": data},
	}, nil
}

// validate 逐项校验参数（未声明键一律拒绝；缺必填/越界/字符集不符 → 错误）。
func validate(ent *entry, args map[string]string) (map[string]any, *envelope.Error) {
	for k := range args {
		if _, ok := ent.fields[k]; !ok {
			return nil, envelope.ED(envelope.CodeForbiddenAction, "unknown argument", k)
		}
	}
	out := map[string]any{}
	for name, spec := range ent.fields {
		v, ok := args[name]
		if !ok {
			return nil, envelope.ED(envelope.CodeBadRequest, "missing argument", name)
		}
		switch spec.kind {
		case kindMAC:
			if !macRe.MatchString(v) {
				return nil, envelope.ED(envelope.CodeBadRequest, "invalid mac", v)
			}
			out[name] = strings.ToLower(v)
		case kindUint:
			if !digitsRe.MatchString(v) {
				return nil, envelope.ED(envelope.CodeBadRequest, "invalid integer", v)
			}
			var n uint64
			for _, c := range []byte(v) {
				n = n*10 + uint64(c-'0')
				if n > 1<<62 {
					break
				}
			}
			if n > spec.max {
				return nil, envelope.ED(envelope.CodeBadRequest, "out of range", v)
			}
			out[name] = json.Number(v)
		case kindHost, kindHostOpt:
			if v == "" {
				if spec.kind == kindHostOpt {
					out[name] = ""
					continue
				}
				return nil, envelope.ED(envelope.CodeBadRequest, "invalid hostname", v)
			}
			if !hostnameRe.MatchString(v) {
				return nil, envelope.ED(envelope.CodeBadRequest, "invalid hostname", v)
			}
			out[name] = v
		}
	}
	return out, nil
}

// execRun 默认执行（无 shell；stdout+stderr 合并）。
func execRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	p, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("executable not found: %s", name)
	}
	c := exec.CommandContext(ctx, p, args...)
	return c.CombinedOutput()
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
