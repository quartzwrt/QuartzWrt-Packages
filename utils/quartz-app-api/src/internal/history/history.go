// Package history 软件包操作记录（WP-5 扩展；DESIGN §4.5）。
//
// 供 App「操作记录」页（UI-DESIGN ⑫）的结构化小读面：升级/卸载/刷新每次
// 执行（成功与失败）写一条记录，环形保留最近 [Cap] 条。
//
//	/etc/quartz-app-api/history.json   (0600；原子写 + flock)
//	  {"items":[{"ts":…,"action":"packages.upgrade","target":"ttyd",
//	             "ok":true,"output":"(1/1) Upgrading …","duration_ms":1234}]}
//
// 与 store 同款并发纪律（插件每调用一进程，读改写都在同一把 flock 下）。
// items 按时间升序存盘，读取时返回最新在前（App 直出）。
package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Cap 环形容量（DESIGN：近 30 条）。
const Cap = 30

// OutputMax 单条输出摘录上限（字符）。
const OutputMax = 512

// Record 一条操作记录。
type Record struct {
	TS         int64  `json:"ts"`
	Action     string `json:"action"` // packages.refresh|upgrade|remove
	Target     string `json:"target,omitempty"`
	OK         bool   `json:"ok"`
	Output     string `json:"output,omitempty"` // stdout/stderr 摘录（尾部）
	DurationMS int64  `json:"duration_ms"`
}

// File 记录根。
type File struct {
	Items []Record `json:"items"`
}

// Store 历史文件句柄。
type Store struct {
	Dir string
}

// New 创建句柄（Dir 为空用默认状态目录）。
func New(dir string) *Store {
	if dir == "" {
		dir = "/etc/quartz-app-api"
	}
	return &Store{Dir: dir}
}

func (s *Store) path() string { return filepath.Join(s.Dir, "history.json") }

// Load 读取全部记录（文件缺失/损坏视为空——历史非关键数据）。
func (s *Store) Load() []Record {
	b, err := os.ReadFile(s.path())
	if err != nil {
		return nil
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return nil
	}
	return f.Items
}

// List 返回最新在前的近 N 条（n<=0 用 Cap）。
func (s *Store) List(n int) []Record {
	if n <= 0 || n > Cap {
		n = Cap
	}
	items := s.Load()
	if len(items) == 0 {
		return nil
	}
	out := make([]Record, 0, n)
	for i := len(items) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, items[i])
	}
	return out
}

// Add 追加一条记录（环形截断、0600、原子写、flock 保护）。
func (s *Store) Add(r Record) error {
	if r.TS == 0 {
		r.TS = time.Now().Unix()
	}
	if len(r.Output) > OutputMax {
		r.Output = r.Output[len(r.Output)-OutputMax:] // 保留尾部（错误多出现在尾部）
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(s.Dir, ".lock.history"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck

	items := s.Load()
	items = append(items, r)
	if len(items) > Cap {
		items = items[len(items)-Cap:]
	}
	b, err := json.MarshalIndent(File{Items: items}, "", " ")
	if err != nil {
		return err
	}
	tmp := s.path() + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, s.path()); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// Tail 输出摘录工具（成功给 stdout 尾部、失败给 stderr 尾部）。
func Tail(b []byte) string {
	s := string(b)
	if len(s) > OutputMax {
		s = s[len(s)-OutputMax:]
	}
	return s
}
