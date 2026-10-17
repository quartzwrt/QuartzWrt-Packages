package actions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quartzwrt/quartz-app-api/internal/envelope"
)

func testExecutor(t *testing.T) (*Executor, *[]string) {
	t.Helper()
	dir := t.TempDir()
	// 造两个假 init 脚本
	for _, n := range []string{"openclash", "dnsmasq"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	calls := &[]string{}
	e := New(dir, nil)
	e.Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		*calls = append(*calls, name+" "+strings.Join(args, " "))
		return []byte("ok"), nil
	}
	return e, calls
}

func TestServiceActions(t *testing.T) {
	e, calls := testExecutor(t)
	for _, action := range []string{"service.start", "service.stop", "service.restart"} {
		res, errE := e.Do(context.Background(), action, map[string]string{"name": "openclash"})
		if errE != nil {
			t.Fatalf("%s: %v", action, errE)
		}
		if res["action"] != action {
			t.Fatalf("result: %v", res)
		}
	}
	want := []string{
		filepath.Join(e.ScriptsDir, "openclash") + " start",
		filepath.Join(e.ScriptsDir, "openclash") + " stop",
		filepath.Join(e.ScriptsDir, "openclash") + " restart",
	}
	if len(*calls) != 3 || (*calls)[2] != want[2] {
		t.Fatalf("calls: %v", *calls)
	}
}

func TestServiceWhitelistAndValidation(t *testing.T) {
	e, calls := testExecutor(t)
	bad := []map[string]string{
		{"name": "dropbear"},                    // 不在白名单
		{"name": "network"},                     // 不在白名单
		{"name": "openclash; rm -rf /"},         // 注入
		{"name": "../etc/passwd"},               // 路径穿越
		{"name": "OPENCLASH"},                   // 大小写
		{"name": ""},                            // 空
		{"name": "openclash --force"},           // 参数注入
		{"name": strings.Repeat("a", 40)},       // 超长
	}
	for _, args := range bad {
		_, errE := e.Do(context.Background(), "service.restart", args)
		if errE == nil || errE.Code != envelope.CodeForbiddenAction {
			t.Fatalf("args %v should be forbidden, got %+v", args, errE)
		}
	}
	// 白名单内但未安装
	_, errE := e.Do(context.Background(), "service.restart", map[string]string{"name": "tailscale"})
	if errE == nil || errE.Code != envelope.CodeForbiddenAction {
		t.Fatalf("not installed: %+v", errE)
	}
	if len(*calls) != 0 {
		t.Fatalf("no execution expected, got %v", *calls)
	}
}

func TestRebootAndDocker(t *testing.T) {
	e, calls := testExecutor(t)
	if _, errE := e.Do(context.Background(), "system.reboot", nil); errE != nil {
		t.Fatalf("reboot: %v", errE)
	}
	if _, errE := e.Do(context.Background(), "docker.container.restart", map[string]string{"id": "abc123"}); errE != nil {
		t.Fatalf("docker: %v", errE)
	}
	// 容器 id 非法
	for _, id := range []string{"", "a b", "abc;ls", strings.Repeat("a", 70)} {
		if _, errE := e.Do(context.Background(), "docker.container.stop", map[string]string{"id": id}); errE == nil || errE.Code != envelope.CodeForbiddenAction {
			t.Fatalf("docker id %q should be forbidden: %+v", id, errE)
		}
	}
	// 未注册动作
	if _, errE := e.Do(context.Background(), "system.poweroff", nil); errE == nil || errE.Code != envelope.CodeForbiddenAction {
		t.Fatalf("unknown action: %+v", errE)
	}
	if len(*calls) != 2 {
		t.Fatalf("calls: %v", *calls)
	}
}

func TestRunFailureMapsToActionFailed(t *testing.T) {
	e, _ := testExecutor(t)
	e.Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("boom"), errors.New("exit 1")
	}
	_, errE := e.Do(context.Background(), "service.restart", map[string]string{"name": "openclash"})
	if errE == nil || errE.Code != envelope.CodeActionFailed || errE.Detail != "boom" {
		t.Fatalf("want action_failed with detail, got %+v", errE)
	}
}

func TestDefaultWhitelistGuardsSelfLockout(t *testing.T) {
	// 默认白名单不得包含自断链路项（DESIGN §5 纪律）
	for _, banned := range []string{"network", "dropbear", "firewall", "uhttpd"} {
		for _, w := range DefaultWhitelist {
			if w == banned {
				t.Fatalf("%s must not be in default whitelist", banned)
			}
		}
	}
}

// bandix（WP-8 bridge 链路的服务面）在默认允许集内——App 重启入口回归防护。
func TestDefaultWhitelistIncludesBandix(t *testing.T) {
	for _, w := range DefaultWhitelist {
		if w == "bandix" {
			return
		}
	}
	t.Fatal("bandix must be in default whitelist (WP-8 service.restart 入口)")
}
