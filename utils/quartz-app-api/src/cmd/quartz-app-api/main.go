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
	"time"

	"github.com/quartzwrt/quartz-app-api/internal/actions"
	"github.com/quartzwrt/quartz-app-api/internal/auth"
	"github.com/quartzwrt/quartz-app-api/internal/config"
	"github.com/quartzwrt/quartz-app-api/internal/daemonfwd"
	"github.com/quartzwrt/quartz-app-api/internal/envelope"
	"github.com/quartzwrt/quartz-app-api/internal/hostinfo"
	"github.com/quartzwrt/quartz-app-api/internal/ratelimit"
	"github.com/quartzwrt/quartz-app-api/internal/store"
)

// version/release 经 -ldflags -X 注入（Makefile GO_PKG_LDFLAGS_X；main 包符号名）。
var (
	version = "dev"
	release = "1"
)

// pluginTimeout 单次插件调用总预算（远低于 rpcd exec 默认 120s，DESIGN §4.4）。
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
		"info":    map[string]any{},
		"login":   map[string]any{"username": "string", "password": "string", "device_name": "string", "client_ip": "string"},
		"refresh": map[string]any{"device_id": "string", "refresh_token": "string"},
		"logout":  map[string]any{"device_id": "string", "refresh_token": "string"},
		"status":  map[string]any{"access_token": "string"},
		"action":  map[string]any{"access_token": "string", "action": "string", "args": map[string]any{}},
		"devices": map[string]any{"access_token": "string"},
		"revoke":  map[string]any{"access_token": "string", "device_id": "string"},
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

	ctx, cancel := context.WithTimeout(context.Background(), pluginTimeout)
	defer cancel()

	switch method {
	case "info":
		info := hostinfo.Info()
		info["features"] = []string{"status", "actions", "devices", "mdns"}
		info["auth"] = []string{"password"}
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
		exec := actions.New("", whitelist(cfg))
		aargs := map[string]string{}
		if m, ok := args["args"].(map[string]any); ok {
			for k, v := range m {
				if s, ok := v.(string); ok {
					aargs[k] = s
				}
			}
		}
		res, ae := exec.Do(ctx, str(args, "action"), aargs)
		if ae != nil {
			return envelope.Err(ae)
		}
		return envelope.OK(res)

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

func reply(p map[string]any, e *envelope.Error) map[string]any {
	if e != nil {
		return envelope.Err(e)
	}
	return p
}

// whitelist 默认集 ∪ uci 追加项（config.WhitelistExtra）。
func whitelist(cfg config.Config) []string {
	out := append([]string{}, actions.DefaultWhitelist...)
	out = append(out, cfg.WhitelistExtra...)
	return out
}

// validateCredentials 生产凭据校验：rpcd session.login 探测 + 立即销毁
// （DESIGN §3.1：复用平台凭据机制，尊重密码变更；探测会话不留残余）。
func validateCredentials(ctx context.Context, username, password string) error {
	if username == "" || password == "" {
		return errors.New("empty credentials")
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
