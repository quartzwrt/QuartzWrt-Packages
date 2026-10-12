package collect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ---- 探针描述文件（DESIGN §13.1）----
//
// 目录：/usr/share/quartz-daemon/probes.d（包内置）+ /etc/quartz-daemon/probes.d
// （本地追加，同 id 覆盖内置）。慢周期重扫：安装新包后 ≤10s 自动出现在
// services 节，无需重编/重启 daemon。声明式（无 shell、绝对路径）。

var probeIDRe = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

// probeSpec 单条探针（v1 三型：procd / pidof / exec）。
type probeSpec struct {
	Type    string   `json:"type"`
	Name    string   `json:"name"`    // procd：服务名（/etc/init.d/<name>）
	Pattern string   `json:"pattern"` // pidof：裸进程名
	Path    string   `json:"path"`    // exec：绝对路径
	Args    []string `json:"args"`    // exec：固定参数
	Regex   string   `json:"regex"`   // exec：输出捕获（捕获组 1 优先）
	Cache   int      `json:"cache"`   // exec：结果缓存秒数（默认 300）
}

// svcDesc 服务探针描述文件。
type svcDesc struct {
	ID      string               `json:"id"`
	Title   string               `json:"title"`
	Running *probeSpec           `json:"running"`
	Extras  map[string]probeSpec `json:"extras"`
}

// loadDescriptors 扫描描述目录（后者覆盖前者），返回有效描述与告警串。
func loadDescriptors(dirs []string) (map[string]*svcDesc, []string) {
	out := map[string]*svcDesc{}
	var warns []string
	for _, dir := range dirs {
		files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		sort.Strings(files)
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				warns = append(warns, fmt.Sprintf("probes: cannot read %s", filepath.Base(f)))
				continue
			}
			var d svcDesc
			if err := json.Unmarshal(b, &d); err != nil {
				warns = append(warns, fmt.Sprintf("probes: invalid descriptor %s", filepath.Base(f)))
				continue
			}
			if err := validateDescriptor(&d); err != nil {
				warns = append(warns, fmt.Sprintf("probes: invalid descriptor %s: %v", filepath.Base(f), err))
				continue
			}
			out[d.ID] = &d
		}
	}
	return out, warns
}

func validateDescriptor(d *svcDesc) error {
	if !probeIDRe.MatchString(d.ID) {
		return fmt.Errorf("bad id")
	}
	if d.Running == nil {
		return fmt.Errorf("missing running probe")
	}
	if err := validateProbe(d.Running); err != nil {
		return err
	}
	for k := range d.Extras {
		p := d.Extras[k]
		if !probeIDRe.MatchString(k) {
			return fmt.Errorf("bad extras key %q", k)
		}
		if err := validateProbe(&p); err != nil {
			return err
		}
	}
	return nil
}

func validateProbe(p *probeSpec) error {
	switch p.Type {
	case "procd":
		if !probeIDRe.MatchString(p.Name) {
			return fmt.Errorf("procd probe needs valid name")
		}
	case "pidof":
		if p.Pattern == "" || strings.ContainsAny(p.Pattern, " \t/") {
			return fmt.Errorf("pidof probe needs bare process name")
		}
	case "exec":
		if !filepath.IsAbs(p.Path) {
			return fmt.Errorf("exec probe needs absolute path")
		}
		if p.Regex != "" {
			if _, err := regexp.Compile(p.Regex); err != nil {
				return fmt.Errorf("bad regex")
			}
		}
		if p.Cache < 0 || p.Cache > 86400 {
			return fmt.Errorf("bad cache")
		}
	default:
		return fmt.Errorf("unknown probe type %q", p.Type)
	}
	return nil
}

// ---- 探针执行 ----

type execResult struct {
	val    string
	ok     bool
	expiry time.Time
}

type serviceRunner struct {
	svcList   map[string]bool // procd 运行态（ubus service list）
	execCache map[string]execResult
	now       func() time.Time
	initdDir  string // /etc/init.d（可注入便于测试）
}

func (r *serviceRunner) initd() string {
	if r.initdDir != "" {
		return r.initdDir
	}
	return "/etc/init.d"
}

// runningState 返回 已装/运行 状态。
//   - procd：已装 = /etc/init.d/<name> 存在；运行 = ubus 实例 running
//   - pidof：仅运行信号（运行即可见；无独立"已装"探测，见 README）
//   - exec：退出码 0 视为运行
func (r *serviceRunner) runningState(p *probeSpec) (installed, running bool) {
	switch p.Type {
	case "procd":
		if _, err := os.Stat(filepath.Join(r.initd(), p.Name)); err != nil {
			return false, false
		}
		return true, r.svcList[p.Name]
	case "pidof":
		out, _ := execTimeout(2*time.Second, "pidof", p.Pattern)
		return strings.TrimSpace(string(out)) != "", true
	case "exec":
		_, err := execTimeout(2*time.Second, p.Path, p.Args...)
		return true, err == nil
	}
	return false, false
}

// extraValue 计算 extras 字段（v1 仅 exec 型；带缓存；失败/缺失 → 字段省略）。
func (r *serviceRunner) extraValue(id, key string, p *probeSpec) (string, bool) {
	if p.Type != "exec" {
		return "", false
	}
	ck := id + "\x00" + key
	if v, ok := r.execCache[ck]; ok && r.now().Before(v.expiry) {
		return v.val, v.ok
	}
	val, ok := "", false
	if out, err := execTimeout(2*time.Second, p.Path, p.Args...); err == nil {
		s := string(out)
		if p.Regex != "" {
			if re, err := regexp.Compile(p.Regex); err == nil {
				if m := re.FindStringSubmatch(s); m != nil {
					if len(m) > 1 {
						val = m[1]
					} else {
						val = m[0]
					}
					ok = val != ""
				}
			}
		} else if t := strings.TrimSpace(firstLine(s)); t != "" {
			val, ok = t, true
		}
	}
	ttl := p.Cache
	if ttl == 0 {
		ttl = 300
	}
	r.execCache[ck] = execResult{val: val, ok: ok, expiry: r.now().Add(time.Duration(ttl) * time.Second)}
	return val, ok
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// buildServices 汇总服务节（契约 §3.6 三态语义）：
// 未装 → 键缺省；已装停止 → {"running": false}；运行中 → {"running": true, ...extras}。
func buildServices(descs map[string]*svcDesc, runner *serviceRunner) map[string]any {
	out := map[string]any{}
	ids := make([]string, 0, len(descs))
	for id := range descs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		d := descs[id]
		installed, running := runner.runningState(d.Running)
		if !installed {
			continue
		}
		val := map[string]any{"running": running}
		if d.Title != "" {
			val["title"] = d.Title
		}
		keys := make([]string, 0, len(d.Extras))
		for k := range d.Extras {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p := d.Extras[k]
			if v, ok := runner.extraValue(id, k, &p); ok {
				val[k] = v
			}
		}
		out[id] = val
	}
	return out
}
