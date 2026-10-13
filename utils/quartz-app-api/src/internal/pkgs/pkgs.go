// Package pkgs 软件包管理面适配层（WP-5 扩展；DESIGN §4.5 / v1.1）。
//
// 数据源 = 路由器 apk 体系（apk-tools 3.x），与 LuCI 软件包页同源命令：
//
//	installed  : apk query --fields … --format json --installed --from system '*'
//	upgradable : apk query --fields … --format json --upgradable '*'（+ installed 映射出旧版本）
//	available  : apk query --fields … --format json --available '<模式>'（含"已安装"标记合并）
//	history    : /etc/quartz-app-api/history.json（internal/history）
//
// 动作（白名单扩展条目，通用 action 方法承载）：
//
//	packages.refresh → apk update         （拉取仓库索引）
//	packages.upgrade → apk upgrade [name] （name 空 = 全部升级）
//	packages.remove  → apk del <name>     （卸载，危险动作；确认层在 App 侧）
//
// 纪律（与 actions 包同款）：参数只做字符集白名单校验，绝不拼接 shell；
// exec.CommandContext 直执行（LookPath 解析 apk）；结果截断进 in-band 信封 +
// 结构化历史（近 30 条）+ logd 审计。
package pkgs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/quartzwrt/quartz-app-api/internal/audit"
	"github.com/quartzwrt/quartz-app-api/internal/envelope"
	"github.com/quartzwrt/quartz-app-api/internal/history"
)

// 超时预算（DESIGN §4.4 v1.1；uhttpd app 实例 /ubus 单次调用总上限 =
// script_timeout×1000ms = 60s——源码核实，本包各动作均须远低于此）。
const (
	QueryTimeout   = 20 * time.Second // 读方法（available '*' 全量约 3.5MB JSON）
	RefreshTimeout = 45 * time.Second // apk update（含下载索引）
	ActionTimeout  = 50 * time.Second // upgrade/remove（含下载安装包）
)

// queryFields apk query 输出字段（与 LuCI 包管理页同尺寸量级；不含 contents
// 等膨胀字段）。字段名经 VM 实测（apk-tools 3.0.5）。
const queryFields = "name,version,description,arch,license,origin,installed-size,file-size"

// nameRe 包名字符集（apk 包名形态；同时防模式注入与 shell 注入）。
var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+_-]{0,127}$`)

// qRe 搜索词字符集（子串搜索；apk 模式的通配符由本包自行包裹，不直通用户输入）。
var qRe = regexp.MustCompile(`^[A-Za-z0-9._+@-]{1,64}$`)

// Runner 执行器（注入测试用）。返回合并 stdout+stderr、退出错误。
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Manager 适配层句柄。
type Manager struct {
	Bin  string // apk 路径（空 = LookPath("apk")，回退 /sbin/apk、/usr/bin/apk）
	Run  Runner // 默认 exec 直执行
	Hist *history.Store
}

// New 构造（dir 为空用默认状态目录）。
func New(dir string) *Manager {
	return &Manager{Bin: "", Run: execRun, Hist: history.New(dir)}
}

// Item 一个软件包条目（三种 scope 的统一超集；App 按存在性渲染）。
type Item struct {
	Name             string `json:"name"`
	Version          string `json:"version"`
	InstalledVersion string `json:"installed_version,omitempty"` // installed 范围=有升级时的目标；upgradable=旧版本
	TargetVersion    string `json:"target_version,omitempty"`    // installed 范围：有升级时的目标版本
	Description      string `json:"description,omitempty"`
	Arch             string `json:"arch,omitempty"`
	License          string `json:"license,omitempty"`
	Origin           string `json:"origin,omitempty"`
	InstalledSize    int64  `json:"installed_size,omitempty"`
	FileSize         int64  `json:"file_size,omitempty"`
	Installed        bool   `json:"installed"`
	Upgradable       bool   `json:"upgradable"`
}

// Page 分页结果。
type Page struct {
	Scope   string `json:"scope"`
	Page    int    `json:"page"`
	PerPage int    `json:"per_page"`
	Total   int    `json:"total"`
	Pages   int    `json:"pages"`
	Items   []Item `json:"items"`
}

// 分页参数边界（DESIGN §4.5）。
const (
	DefaultPerPage = 24
	MaxPerPage     = 100
)

// Scopes 支持的读范围。
var Scopes = []string{"installed", "upgradable", "available", "history"}

// List 读方法核心：scope ∈ Scopes；q 子串搜索（installed/available 有效）；
// page/perPage 分页（历史 scope 忽略分页，返回最新在前 ≤30 条）。
func (m *Manager) List(ctx context.Context, scope, q string, page, perPage int) (*Page, *envelope.Error) {
	switch scope {
	case "installed", "upgradable", "available":
	case "history":
		recs := m.Hist.List(history.Cap)
		items := make([]Item, 0, len(recs))
		for _, r := range recs {
			items = append(items, histItem(r))
		}
		pages := 0
		if len(items) > 0 {
			pages = 1
		}
		return &Page{Scope: scope, Page: 1, PerPage: len(items), Total: len(items), Pages: pages, Items: items}, nil
	default:
		return nil, envelope.ED(envelope.CodeBadRequest, "unknown scope", scope)
	}

	pattern := "*"
	if q != "" {
		if !qRe.MatchString(q) {
			return nil, envelope.ED(envelope.CodeBadRequest, "invalid query", q)
		}
		pattern = "*" + strings.ToLower(q) + "*"
	}

	var items []Item
	switch scope {
	case "installed":
		installed, err := m.query(ctx, installedArgs(pattern)...)
		if err != nil {
			return nil, queryErr(err)
		}
		targets := m.upgradableMap(ctx) // 目标版本映射（失败视为"无升级"，不致命）
		for i := range installed {
			installed[i].Installed = true
			if tv, ok := targets[installed[i].Name]; ok {
				installed[i].Upgradable = true
				installed[i].TargetVersion = tv
			}
		}
		items = installed
	case "upgradable":
		up, err := m.query(ctx, upgradableArgs(pattern)...)
		if err != nil {
			return nil, queryErr(err)
		}
		old := m.installedVersionMap(ctx)
		for i := range up {
			up[i].Installed = true
			up[i].Upgradable = true
			up[i].InstalledVersion = old[up[i].Name]
		}
		items = up
	case "available":
		avail, err := m.query(ctx, availableArgs(pattern)...)
		if err != nil {
			return nil, queryErr(err)
		}
		old := m.installedVersionMap(ctx)
		for i := range avail {
			inst, ok := old[avail[i].Name]
			if !ok {
				continue
			}
			avail[i].Installed = true
			avail[i].InstalledVersion = inst // 已安装版本（含与仓库版一致）
			if inst != avail[i].Version {
				avail[i].Upgradable = true
			}
		}
		items = avail
	}

	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	page, perPage, total, pages, start, end := paginate(len(items), page, perPage)
	out := items[start:end]
	if out == nil {
		out = []Item{}
	}
	return &Page{Scope: scope, Page: page, PerPage: perPage, Total: total, Pages: pages, Items: out}, nil
}

// Action 执行动作（packages.refresh / packages.upgrade / packages.remove）；
// 一切结果（含失败）写结构化历史 + 审计。
func (m *Manager) Action(ctx context.Context, action, name string) (map[string]any, *envelope.Error) {
	var (
		args    []string
		timeout time.Duration
		target  = name
	)
	switch action {
	case "packages.refresh":
		args = []string{"update"}
		timeout = RefreshTimeout
		target = ""
	case "packages.upgrade":
		args = []string{"upgrade"}
		timeout = ActionTimeout
		if name != "" {
			if !nameRe.MatchString(name) {
				return nil, envelope.ED(envelope.CodeForbiddenAction, "invalid package name", name)
			}
			args = append(args, name)
		}
	case "packages.remove":
		if name == "" {
			return nil, envelope.E(envelope.CodeBadRequest, "package name required")
		}
		if !nameRe.MatchString(name) {
			return nil, envelope.ED(envelope.CodeForbiddenAction, "invalid package name", name)
		}
		args = []string{"del", name}
		timeout = ActionTimeout
	default:
		return nil, envelope.ED(envelope.CodeForbiddenAction, "unknown action", action)
	}

	start := time.Now()
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := m.run(cctx, args...)
	dur := time.Since(start)

	ok := err == nil
	rec := history.Record{
		Action:     action,
		Target:     target,
		OK:         ok,
		Output:     history.Tail(out),
		DurationMS: dur.Milliseconds(),
	}
	if herr := m.Hist.Add(rec); herr != nil {
		audit.Logf("packages history write failed action=%s err=%v", action, herr)
	}
	if ok {
		audit.Logf("packages action=%s target=%s ok dur_ms=%d", action, target, dur.Milliseconds())
		return map[string]any{
			"action": action,
			"result": map[string]any{
				"stdout":      truncate(string(out), 4096),
				"duration_ms": dur.Milliseconds(),
			},
		}, nil
	}
	detail := classify(out, err)
	audit.Logf("packages action=%s target=%s fail dur_ms=%d detail=%q", action, target, dur.Milliseconds(), truncate(detail, 200))
	// 超时（context deadline）→ 明确报"超时"，其余带 apk 输出尾部。
	code := envelope.CodeActionFailed
	if cctx.Err() == context.DeadlineExceeded {
		detail = fmt.Sprintf("timeout after %ds: %s", int(timeout.Seconds()), detail)
	}
	return nil, envelope.ED(code, "package action failed", detail)
}

// ---- apk 命令构造 ----

func (m *Manager) query(ctx context.Context, args ...string) ([]Item, error) {
	out, err := m.run(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, classify(out, err))
	}
	// 宽容解析：apk 可能附加 WARNING（stderr 已合并）；截取首个 '[' 起始的 JSON。
	i := bytes.IndexByte(out, '[')
	if i < 0 {
		return nil, fmt.Errorf("apk query: no JSON output: %s", truncate(string(out), 200))
	}
	var raw []struct {
		Name          string `json:"name"`
		Version       string `json:"version"`
		Description   string `json:"description"`
		Arch          string `json:"arch"`
		License       string `json:"license"`
		Origin        string `json:"origin"`
		InstalledSize int64  `json:"installed-size"`
		FileSize      int64  `json:"file-size"`
	}
	if err := json.Unmarshal(out[i:], &raw); err != nil {
		return nil, fmt.Errorf("apk query: bad JSON: %w", err)
	}
	items := make([]Item, 0, len(raw))
	for _, r := range raw {
		if r.Name == "" {
			continue
		}
		items = append(items, Item{
			Name:          r.Name,
			Version:       r.Version,
			Description:   r.Description,
			Arch:          r.Arch,
			License:       r.License,
			Origin:        r.Origin,
			InstalledSize: r.InstalledSize,
			FileSize:      r.FileSize,
		})
	}
	return items, nil
}

func installedArgs(pattern string) []string {
	return []string{"query", "--fields", queryFields, "--format", "json", "--installed", "--from", "system", pattern}
}

func upgradableArgs(pattern string) []string {
	return []string{"query", "--fields", queryFields, "--format", "json", "--upgradable", pattern}
}

func availableArgs(pattern string) []string {
	return []string{"query", "--fields", queryFields, "--format", "json", "--available", pattern}
}

// upgradableMap 有升级的包 → 目标版本（供 installed 范围打升级标）。
func (m *Manager) upgradableMap(ctx context.Context) map[string]string {
	items, err := m.query(ctx, upgradableArgs("*")...)
	if err != nil {
		return nil
	}
	out := make(map[string]string, len(items))
	for _, it := range items {
		out[it.Name] = it.Version
	}
	return out
}

// installedVersionMap 已安装包 → 版本（供 available/upgradable 范围合并）。
func (m *Manager) installedVersionMap(ctx context.Context) map[string]string {
	items, err := m.query(ctx, installedArgs("*")...)
	if err != nil {
		return nil
	}
	out := make(map[string]string, len(items))
	for _, it := range items {
		out[it.Name] = it.Version
	}
	return out
}

// histItem 历史记录 → 统一条目形态（App 解析同一 items 数组）。
func histItem(r history.Record) Item {
	return Item{
		Name:        r.Action, // 动作 id（App 映射文案）
		Version:     r.Target, // 目标包名（可为空）
		Description: r.Output,
		FileSize:    r.DurationMS,
		Installed:   r.OK,
	}
}

// ---- 分页 / 错误 / 执行 ----

func paginate(total, page, perPage int) (p, pp, tot, pages, start, end int) {
	if perPage <= 0 {
		perPage = DefaultPerPage
	}
	if perPage > MaxPerPage {
		perPage = MaxPerPage
	}
	pages = boolPages(total, perPage)
	if page <= 0 {
		page = 1
	}
	if page > pages {
		page = pages
	}
	if page < 1 {
		page = 1
	}
	start = (page - 1) * perPage
	if start > total {
		start = total
	}
	end = start + perPage
	if end > total {
		end = total
	}
	return page, perPage, total, pages, start, end
}

func boolPages(total, perPage int) int {
	if perPage <= 0 || total == 0 {
		return 0
	}
	return (total + perPage - 1) / perPage
}

func queryErr(err error) *envelope.Error {
	msg := err.Error()
	return envelope.ED(envelope.CodeDataUnavailable, "package query failed", truncate(msg, 400))
}

// classify 从合并输出提取可读错误：apk 的 ERROR 行（含其后缩进续行）优先，
// 否则取尾部若干非空行；错误多出现在输出尾部。
func classify(out []byte, err error) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var picked []string
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if !strings.HasPrefix(l, "ERROR:") && !strings.Contains(l, "UNTRUSTED") {
			continue
		}
		picked = append(picked, l)
		for j := i + 1; j < len(lines) && len(picked) < 4; j++ {
			if lines[j] == "" || !strings.HasPrefix(lines[j], " ") {
				break
			}
			picked = append(picked, strings.TrimSpace(lines[j]))
		}
		if len(picked) >= 4 {
			break
		}
	}
	if len(picked) == 0 {
		for i := len(lines) - 1; i >= 0 && len(picked) < 3; i-- {
			if strings.TrimSpace(lines[i]) != "" {
				picked = append([]string{lines[i]}, picked...)
			}
		}
	}
	msg := strings.Join(picked, " | ")
	if msg == "" {
		msg = err.Error()
	}
	return truncate(msg, 400)
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// execRun 默认执行：LookPath 解析 apk（rpcd 子进程 PATH 可能极简，带绝对路径回退）；
// stdout+stderr 合并（apk 的 WARNING/ERROR 走 stderr，须入报告与历史）。
func execRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	p, err := exec.LookPath(name)
	if err != nil {
		for _, cand := range []string{"/sbin/" + name, "/usr/bin/" + name, "/bin/" + name, "/usr/sbin/" + name} {
			if fi, serr := os.Stat(cand); serr == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
				p = cand
				break
			}
		}
	}
	if p == "" {
		return nil, fmt.Errorf("executable not found: %s", name)
	}
	c := exec.CommandContext(ctx, p, args...)
	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf
	err = c.Run()
	return buf.Bytes(), err
}

// run 经注入执行器执行（Bin 为空用 "apk"）。
func (m *Manager) run(ctx context.Context, args ...string) ([]byte, error) {
	bin := m.Bin
	if bin == "" {
		bin = "apk"
	}
	return m.Run(ctx, bin, args...)
}
