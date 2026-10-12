// Package output 负责快照落盘：目录自建 + 临时文件 + rename 原子写。
package output

import (
	"os"
	"path/filepath"
)

// EnsureDir 创建快照目录（/var/run/quartz 位于 tmpfs，重启即清空，须启动自建）。
func EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

// Writer 原子写器：写同目录临时文件 → chmod 0644 → rename。
//
// 目录为 tmpfs：rename 原子性即足，不做 fsync（无落盘语义可保）。
type Writer struct {
	Path string
}

// New 创建写器。
func New(path string) *Writer { return &Writer{Path: path} }

// Write 原子写入快照字节（失败时清理临时文件，不改动既有快照）。
func (w *Writer) Write(b []byte) error {
	dir := filepath.Dir(w.Path)
	tmp := filepath.Join(dir, "."+filepath.Base(w.Path)+".tmp")
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
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
	// umask 可能压缩权限，显式 Chmod 保证 0644（契约 §6.1）
	if err := os.Chmod(tmp, 0o644); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, w.Path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
