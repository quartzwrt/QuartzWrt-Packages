// quartz-app-api — QuartzWrt 专属 App API（rpcd exec 插件，WP-5）。
//
// 设计规范: docs/DESIGN.md v1.0（定案）
// 数据契约: luci-app-dashboard/docs/AGGREGATE-API.md（冻结 v1，本插件为代理透传）
//
// rpcd exec 插件协议（DESIGN §1.1）：
//
//	quartz-app-api list              → stdout 输出方法表 JSON（rpcd 启动时注册）
//	quartz-app-api call <method>     → stdin 读参数 JSON，stdout 输出单个 JSON 对象
//
// 注意：stdout 必须是纯 JSON（任何杂散输出会破坏解析）；stderr 被 rpcd 吞掉，
// 审计统一走 logd（internal/audit）。方法级错误一律 in-band（envelope）。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/quartzwrt/quartz-app-api/internal/actions"
	"github.com/quartzwrt/quartz-app-api/internal/auth"
	"github.com/quartzwrt/quartz-app-api/internal/config"
	"github.com/quartzwrt/quartz-app-api/internal/daemonfwd"
	"github.com/quartzwrt/quartz-app-api/internal/envelope"
	"github.com/quartzwrt/quartz-app-api/internal/hostinfo"
	"github.com/quartzwrt/quartz-app-api/internal/pkgs"
	"github.com/quartzwrt/quartz-app-api/internal/ratelimit"
	"github.com/quartzwrt/quartz-app-api/internal/store"
)

// version/release 经 -ldflags -X 注入（Makefile GO_PKG_LDFLAGS_X；main 包符号名）。
var (
	version = "dev"
	release = "1"
)

// pluginTimeout 单次插件调用默认预算（远低于 rpcd exec 默认 120s，DESIGN §4.4）。
const pluginTimeout = 10 * time.Second

func main() {
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "--version", "-v":
			fmt.Printf("quartz-app-api %s-%s\n", version, release)
			return
		case "list":
			emit(methodList())
			return
		case "call":
			if len(os.Args) < 3 {
				emit(envelope.Err(envelope.E(envelope.CodeBadRequest, "missing method")))
				return
			}
			emit(dispatch(os.Args[2], readArgs()))
			return
		}
	}
	fmt.Fprintln(os.Stderr, "usage: quartz-app-api list | call <method> | --version")
	os.Exit(1)
}

// methodList rpcd 注册用方法表（键=方法名，值=参数名→类型；类型仅作 ubus 签名展示）。
func methodList() map[string]any {
	return map[string]any{
		"info":     map[string]any{},
		"login":    map[string]any{"username": "string", "password": "string", "device_name": "string", "client_ip": "string"},
		"refresh":  map[string]any{"device_id": "string", "refresh_token": "string"},
		"logout":   map[string]any{"device_id": "string", "refresh_token": "string"},
		"status":   map[string]any{"access_token": "string"},
		"action":   map[string]any{"access_token": "string", "action": "string", "args": map[string]any{}},
		"devices":  map[string]any{"access_token": "string"},
		"revoke":   map[string]any{"access_token": "string", "device_id": "string"},
		"packages": map[string]any{"access_token": "string", "scope": "string", "query": "string", "page": "Integer", "per_page": "Integer"},
	}
}

// readArgs 读 stdin 参数（rpcd 注入的 ubus_rpc_session 等未知键一律忽略）。
func readArgs() map[string]any {
	b, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	m := map[string]any{}
	if len(b) > 0 {
		_ = json.Unmarshal(b, &m)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m
}

// emit 输出单个 JSON 对象（stdout 唯一出口）。
func emit(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		b = []byte(`{"ok":false,"api":1,"error":{"code":"internal","message":"marshal failed"}}`)
	}
	_, _ = os.Stdout.Write(append(b, '\n'))
}

func str(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// intOf 读整型参数（JSON 反序列化为 float64；缺省 0 交由层内默认值处理）。
func intOf(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return 0
}

// budget 每方法超时预算（DESIGN §4.4 v1.1）。packages.* 动作由 pkgs 内部
// 细分（refresh 45s / upgrade|remove 50s），外层各留 2s 余量；结果仍须落在
// uhttpd app 实例 script_timeout=60s 的单次 /ubus 调用窗口内。
func budget(method string, args map[string]any) time.Duration {
	switch method {
	case "packages":
		return pkgs.QueryTimeout + 2*time.Second
	case "action":
		if strings.HasPrefix(str(args, "action"), "packages.") {
			return pkgs.ActionTimeout + 2*time.Second
		}
	}
	return pluginTimeout
}

// dispatch 方法分派（panic 兜底为 internal 信封；始终 exit 0）。
func dispatch(method string, args map[string]any) (resp map[string]any) {
	defer func() {
		if r := recover(); r != nil {
			resp = envelope.Err(envelope.ED(envelope.CodeInternal, "internal error", fmt.Sprint(r)))
		}
	}()

	cfg := config.Load()
	svc := auth.New(
		store.New(store.DefaultDir),
		ratelimit.New("", ratelimit.Config{Fails: cfg.LoginFails, Base: cfg.LockoutBase}),
		cfg,
		validateCredentials,
	)
	svc.Router = hostinfo.Info

	// 每方法预算（DESIGN §4.4 v1.1）：默认 10s；packages 读 20s；packages.*
	// 动作由 pkgs 包内部按操作细分（refresh 45s / upgrade|remove 50s），
	// 外层再留余量。上限受 uhttpd app 实例 /ubus 单次调用 60s 约束（源码核实）。
	ctx, cancel := context.WithTimeout(context.Background(), budget(method, args))
	defer cancel()

	switch method {
	case "info":
		info := hostinfo.Info()
		info["features"] = []string{"status", "actions", "devices", "mdns", "packages"}
		info["auth"] = []string{"password"}
		// 根口令是否已设（未设时 rpcd session.login 接受任意密码——
		// DESIGN v1.0.2 / WORKPLAN 坑位 #28；App 应在配对前提示）
		info["password_set"] = rootPasswordSet()
		return envelope.OK(info)

	case "login":
		p, e := svc.Login(ctx, str(args, "username"), str(args, "password"), str(args, "device_name"), str(args, "client_ip"))
		return reply(p, e)

	case "refresh":
		p, e := svc.Refresh(str(args, "device_id"), str(args, "refresh_token"))
		return reply(p, e)

	case "logout":
		p, e := svc.Logout(str(args, "device_id"), str(args, "refresh_token"))
		return reply(p, e)

	case "status":
		if _, e := svc.Authenticate(str(args, "access_token")); e != nil {
			return envelope.Err(e)
		}
		raw, err := daemonfwd.New(cfg.DaemonURL).Status(ctx)
		if err != nil {
			return envelope.Err(envelope.ED(envelope.CodeDataUnavailable, "daemon snapshot unavailable", err.Error()))
		}
		return envelope.OK(map[string]any{"snapshot": raw})

	case "action":
		if _, e := svc.Authenticate(str(args, "access_token")); e != nil {
			return envelope.Err(e)
		}
		actionID := str(args, "action")
		aargs := map[string]string{}
		if m, ok := args["args"].(map[string]any); ok {
			for k, v := range m {
				if s, ok := v.(string); ok {
					aargs[k] = s
				}
			}
		}
		// 软件包动作（WP-5 扩展；同一 action 方法 + 白名单前缀路由）。
		if strings.HasPrefix(actionID, "packages.") {
			mgr := pkgs.New(store.DefaultDir)
			res, pe := mgr.Action(ctx, actionID, aargs["name"])
			if pe != nil {
				return envelope.Err(pe)
			}
			return envelope.OK(res)
		}
		exec := actions.New("", whitelist(cfg))
		res, ae := exec.Do(ctx, actionID, aargs)
		if ae != nil {
			return envelope.Err(ae)
		}
		return envelope.OK(res)

	case "packages":
		if _, e := svc.Authenticate(str(args, "access_token")); e != nil {
			return envelope.Err(e)
		}
		mgr := pkgs.New(store.DefaultDir)
		pg, pe := mgr.List(ctx, str(args, "scope"), str(args, "query"), intOf(args, "page"), intOf(args, "per_page"))
		if pe != nil {
			return envelope.Err(pe)
		}
		return envelope.OK(map[string]any{
			"scope":    pg.Scope,
			"page":     pg.Page,
			"per_page": pg.PerPage,
			"total":    pg.Total,
			"pages":    pg.Pages,
			"items":    pg.Items,
		})

	case "devices":
		dev, e := svc.Authenticate(str(args, "access_token"))
		if e != nil {
			return envelope.Err(e)
		}
		list, e := svc.ListDevices(dev.ID)
		if e != nil {
			return envelope.Err(e)
		}
		return envelope.OK(map[string]any{"devices": list})

	case "revoke":
		dev, e := svc.Authenticate(str(args, "access_token"))
		if e != nil {
			return envelope.Err(e)
		}
		if e := svc.Revoke(dev.ID, str(args, "device_id")); e != nil {
			return envelope.Err(e)
		}
		return envelope.OK(map[string]any{})

	default:
		return envelope.Err(envelope.ED(envelope.CodeBadRequest, "unknown method", method))
	}
}

// reply 统一封装信封（auth 层全部方法返回**裸载荷**；错误已带信封）。
func reply(p map[string]any, e *envelope.Error) map[string]any {
	if e != nil {
		return envelope.Err(e)
	}
	if p == nil {
		p = map[string]any{}
	}
	return envelope.OK(p)
}

// whitelist 默认集 ∪ uci 追加项（config.WhitelistExtra）。
func whitelist(cfg config.Config) []string {
	out := append([]string{}, actions.DefaultWhitelist...)
	out = append(out, cfg.WhitelistExtra...)
	return out
}

// validateCredentials 生产凭据校验：rpcd session.login 探测 + 立即销毁
// （DESIGN §3.1：复用平台凭据机制，尊重密码变更；探测会话不留残余）。
//
// 前置：要求根口令已设置（DESIGN v1.0.2 / WORKPLAN 坑位 #28——shadow 空口令
// 时 rpcd 接受任意密码，会签发长效凭据，必须显式拒绝）。
func validateCredentials(ctx context.Context, username, password string) error {
	if username == "" || password == "" {
		return errors.New("empty credentials")
	}
	if !rootPasswordSet() {
		return errors.New("router password not set")
	}
	req, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		return err
	}
	out, err := ubusCommand(ctx, "call", "session", "login", string(req)).Output()
	if err != nil {
		return err
	}
	var resp struct {
		SID string `json:"ubus_rpc_session"`
	}
	if err := json.Unmarshal(out, &resp); err != nil || resp.SID == "" {
		return errors.New("login rejected")
	}
	destroy, _ := json.Marshal(map[string]string{"ubus_rpc_session": resp.SID})
	_ = ubusCommand(ctx, "call", "session", "destroy", string(destroy)).Run()
	return nil
}

// rootPasswordSet 读 /etc/shadow 判断 root 口令（读失败时按"已设"放行，
// 以 session.login 结果为准）。
func rootPasswordSet() bool {
	b, err := os.ReadFile("/etc/shadow")
	if err != nil {
		return true
	}
	return auth.RootPasswordSet(string(b))
}

// ubusCommand 构造 ubus CLI 命令（PATH 查找，回退常见绝对路径——rpcd 子进程
// 环境 PATH 可能极简）。
func ubusCommand(ctx context.Context, args ...string) *exec.Cmd {
	p, err := exec.LookPath("ubus")
	if err != nil {
		for _, cand := range []string{"/bin/ubus", "/usr/bin/ubus", "/sbin/ubus"} {
			if _, serr := os.Stat(cand); serr == nil {
				p = cand
				break
			}
		}
	}
	if p == "" {
		p = "ubus"
	}
	return exec.CommandContext(ctx, p, args...)
}
