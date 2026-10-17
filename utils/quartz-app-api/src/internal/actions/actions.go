// Package actions 控制面白名单动作表（DESIGN §5、WP-4 DESIGN §12.4 动作路径表）。
//
// 纪律：**动作只按 id 显式注册**（无任意 ubus 透传）；参数仅结构校验 + 字符集
// 白名单，绝不拼接进 shell（exec.Command 直执行）；执行走 /etc/init.d/<name> 或
// ubus/docker CLI（LookPath 解析）。
package actions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"github.com/quartzwrt/quartz-app-api/internal/envelope"
)

// Timeout 单动作执行上限（DESIGN §4.4）。
const Timeout = 5 * time.Second

// MaxOutput 返回 stdout 截断上限。
const MaxOutput = 4096

// DefaultWhitelist 默认允许集（不带 network/dropbear 等自断链路项，DESIGN §5）。
// bandix = WP-8 bridge 链路的服务面（重启/启停走本表；写动作走 ubus 转发项，2026-10-17）。
var DefaultWhitelist = []string{"openclash", "dockerd", "dnsmasq", "quartz-daemon", "tailscale", "zerotier", "bandix"}

var (
	initNameRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)
	dockerIDRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	errNotAllow = errors.New("not allowed")
)

// Executor 动作执行器。
type Executor struct {
	ScriptsDir string
	Whitelist  map[string]bool
	// Run 可注入（测试用）；默认 exec.CommandContext（无 shell）。
	Run func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// New 构造执行器（whitelist 为 nil 时用 DefaultWhitelist；scriptsDir 空用 /etc/init.d）。
func New(scriptsDir string, whitelist []string) *Executor {
	if scriptsDir == "" {
		scriptsDir = "/etc/init.d"
	}
	if whitelist == nil {
		whitelist = DefaultWhitelist
	}
	wl := map[string]bool{}
	for _, n := range whitelist {
		wl[n] = true
	}
	return &Executor{ScriptsDir: scriptsDir, Whitelist: wl, Run: execRun}
}

// Do 执行动作（action 形如 "service.restart"）。
func (e *Executor) Do(ctx context.Context, action string, args map[string]string) (map[string]any, *envelope.Error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	var (
		out []byte
		err error
	)
	switch action {
	case "service.start", "service.stop", "service.restart":
		op := action[len("service."):]
		name := args["name"]
		if !initNameRe.MatchString(name) || !e.Whitelist[name] {
			return nil, envelope.ED(envelope.CodeForbiddenAction, "service not allowed", name)
		}
		script := filepath.Join(e.ScriptsDir, name)
		if fi, serr := os.Stat(script); serr != nil || fi.IsDir() {
			return nil, envelope.ED(envelope.CodeForbiddenAction, "service not installed", name)
		}
		out, err = e.Run(ctx, script, op)

	case "system.reboot":
		out, err = e.Run(ctx, "ubus", "call", "system", "reboot")

	case "docker.container.start", "docker.container.stop", "docker.container.restart":
		op := action[len("docker.container."):]
		id := args["id"]
		if !dockerIDRe.MatchString(id) {
			return nil, envelope.ED(envelope.CodeForbiddenAction, "invalid container id", id)
		}
		out, err = e.Run(ctx, "docker", "container", op, id)

	default:
		return nil, envelope.ED(envelope.CodeForbiddenAction, "unknown action", action)
	}

	if err != nil {
		detail := truncate(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return nil, envelope.ED(envelope.CodeActionFailed, "action failed", detail)
	}
	return map[string]any{"action": action, "result": map[string]any{"stdout": truncate(string(out))}}, nil
}

// Executable 预检：动作依赖的二进制是否可用（信息面，供 App 判定显示）。
func Executable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// execRun 默认执行（无 shell；stdout+stderr 合并，环境继承 rpcd）。
func execRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	p, err := exec.LookPath(name)
	if err != nil {
		// init 脚本通常带绝对路径（/etc/init.d/xxx 可执行）；此处兼容裸名
		return nil, fmt.Errorf("executable not found: %s", name)
	}
	c := exec.CommandContext(ctx, p, args...)
	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf
	err = c.Run()
	return buf.Bytes(), err
}

func truncate(s string) string {
	if len(s) > MaxOutput {
		return s[:MaxOutput] + "…(truncated)"
	}
	return s
}
