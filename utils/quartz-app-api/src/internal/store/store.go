// Package store 管理 quartz-app-api 的持久状态（DESIGN §3.4）。
//
//	/etc/quartz-app-api/secret      32B 随机 HMAC 密钥（0600）
//	/etc/quartz-app-api/state.json  设备注册表（0600；原子写 + flock）
//
// 插件为「每调用一进程」（rpcd exec 模型），全部读改写经 Update() 在同一把
// flock 下完成，防并发登录/刷新互相覆盖。
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// DefaultDir 运行期状态目录（overlay，持久）。
const DefaultDir = "/etc/quartz-app-api"

// Device 设备注册表条目（refresh 只存哈希，DESIGN §3.1/§3.4）。
type Device struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Created         int64  `json:"created"`
	LastSeen        int64  `json:"last_seen"`
	LastIP          string `json:"last_ip"`
	RefreshSHA256   string `json:"refresh_sha256"`                // 当前 refresh 的哈希
	PrevRefreshHash string `json:"prev_refresh_sha256,omitempty"` // 上一枚（复用检测）
	RefreshExp      int64  `json:"refresh_exp"`
	RotCounter      int    `json:"rot_counter"`
	Revoked         bool   `json:"revoked"`
	// ConfigSID / ConfigSIDExp：配置面（WP-6 W6.10 `config.*`）所需的 rpcd 会话——
	// rpcd 的 uci 写入与 apply/confirm 必须在同一会话内（会话私有 delta 目录），
	// 而 app-api 是 rpcd exec 插件、每次调用都是新进程 → sid 必须持久化。
	// 会话由 login 时用管理员凭据开启（ACL 全量），logout/revoke 时销毁。
	ConfigSID    string `json:"config_sid,omitempty"`
	ConfigSIDExp int64  `json:"config_sid_exp,omitempty"`
}

// State 注册表根。
type State struct {
	Devices []Device `json:"devices"`
}

// Find 按 id 查找设备。
func (s *State) Find(id string) *Device {
	for i := range s.Devices {
		if s.Devices[i].ID == id {
			return &s.Devices[i]
		}
	}
	return nil
}

// Store 状态目录句柄。
type Store struct {
	Dir string
}

// New 创建句柄（Dir 为空时用 DefaultDir）。
func New(dir string) *Store {
	if dir == "" {
		dir = DefaultDir
	}
	return &Store{Dir: dir}
}

func (s *Store) path(name string) string { return filepath.Join(s.Dir, name) }

// EnsureDir 建立状态目录（0700）。
func (s *Store) EnsureDir() error {
	return os.MkdirAll(s.Dir, 0o700)
}

// Secret 读取 HMAC 密钥；不存在则以 crypto/rand 生成（32B hex，0600）。
// 并发安全：O_CREATE|O_EXCL 落败（EEXIST）时重读。
func (s *Store) Secret() ([]byte, error) {
	if err := s.EnsureDir(); err != nil {
		return nil, err
	}
	p := s.path("secret")
	if b, err := os.ReadFile(p); err == nil && len(b) > 0 {
		return decodeSecret(b)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	enc := []byte(hex.EncodeToString(raw))
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil, rerr
			}
			return decodeSecret(b)
		}
		return nil, err
	}
	if _, err := f.Write(enc); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return raw, nil
}

func decodeSecret(b []byte) ([]byte, error) {
	raw, err := hex.DecodeString(string(trimSpace(b)))
	if err != nil || len(raw) == 0 {
		return nil, fmt.Errorf("bad secret file: %w", err)
	}
	return raw, nil
}

func trimSpace(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && (b[i] == ' ' || b[i] == '\n' || b[i] == '\r' || b[i] == '\t') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\n' || b[j-1] == '\r' || b[j-1] == '\t') {
		j--
	}
	return b[i:j]
}

// Load 读取注册表（文件缺失视为空表）。
func (s *Store) Load() (State, error) {
	var st State
	b, err := os.ReadFile(s.path("state.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return st, nil
		}
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return st, fmt.Errorf("state.json corrupt: %w", err)
	}
	return st, nil
}

// Update 在 flock 保护下执行读-改-写；fn 返回错误则不落盘。
func (s *Store) Update(fn func(*State) error) (State, error) {
	var out State
	if err := s.EnsureDir(); err != nil {
		return out, err
	}
	lock, err := os.OpenFile(s.path(".lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return out, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return out, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck

	st, err := s.Load()
	if err != nil {
		return out, err
	}
	if err := fn(&st); err != nil {
		return out, err
	}
	if err := s.save(&st); err != nil {
		return out, err
	}
	return st, nil
}

// save 原子写 state.json（临时文件 + rename，0600；flock 由 Update 持有）。
func (s *Store) save(st *State) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(".state.json.tmp")
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
	if err := os.Rename(tmp, s.path("state.json")); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// NewDeviceID 生成设备 id（8 字节随机 hex）。
func NewDeviceID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}
