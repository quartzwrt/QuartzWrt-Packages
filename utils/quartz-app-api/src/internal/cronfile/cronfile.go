// Package cronfile 系统 crontab（/etc/crontabs/root）的受限读写。
//
// 计划任务不是 UCI 配置：LuCI 的 crontab 页直接 fs.read/fs.write 该文件，保存后
// `/etc/init.d/cron reload`。App 侧走本包的固定路径 + 大小/控制字符校验，避免
// 把任意文件读写暴露成通用 API；内容仍由用户负责（就是可执行的 cron 行）。
package cronfile

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/quartzwrt/quartz-app-api/internal/envelope"
)

const (
	// Path 固定系统 crontab（不接收调用方路径）。
	Path = "/etc/crontabs/root"
	// MaxBytes 单文件上限（LuCI 页面是 25 行文本框，64KB 足够）。
	MaxBytes = 64 * 1024
	// MaxLines 行数上限。
	MaxLines = 1000
	// MaxLineBytes 单行上限（防止一行塞入超大脚本）。
	MaxLineBytes = 4096
)

// File 读取结果。
type File struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Exists  bool   `json:"exists"`
}

// Read 读取固定 crontab；不存在时返回 exists=false（不是错误）。
func Read(path string) (File, *envelope.Error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return File{Path: path}, nil
		}
		return File{}, envelope.ED(envelope.CodeDataUnavailable, "cannot read crontab", err.Error())
	}
	return File{Path: path, Content: string(b), Exists: true}, nil
}

// Normalize 与 LuCI crontab 页一致：CRLF→LF、去首尾空白、末尾补一个换行。
func Normalize(content string) string {
	s := strings.ReplaceAll(content, "\r\n", "\n")
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return s + "\n"
}

// Validate 校验规范化后的内容；空内容合法（清空计划任务）。
func Validate(content string) *envelope.Error {
	if len(content) > MaxBytes {
		return envelope.ED(envelope.CodeBadRequest, "crontab too large", "")
	}
	lines := strings.Split(content, "\n")
	if len(lines) > MaxLines {
		return envelope.ED(envelope.CodeBadRequest, "too many crontab lines", "")
	}
	for _, r := range content {
		if r == '\n' || r == '\t' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return envelope.ED(envelope.CodeBadRequest, "control character in crontab", "")
		}
	}
	for _, line := range lines {
		if len(line) > MaxLineBytes {
			return envelope.ED(envelope.CodeBadRequest, "crontab line too long", "")
		}
	}
	return nil
}

// Write 校验后原子写入固定路径（同目录临时文件 + rename，0600）。
// 返回写入字节数与非空行数（审计用，不记录内容）。
func Write(path, content string) (int, int, *envelope.Error) {
	content = Normalize(content)
	if ee := Validate(content); ee != nil {
		return 0, 0, ee
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, 0, envelope.ED(envelope.CodeActionFailed, "cannot create crontab dir", err.Error())
	}
	tmp, err := os.CreateTemp(dir, "root.tmp-*")
	if err != nil {
		return 0, 0, envelope.ED(envelope.CodeActionFailed, "cannot create temp crontab", err.Error())
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return 0, 0, envelope.ED(envelope.CodeActionFailed, "cannot chmod crontab", err.Error())
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return 0, 0, envelope.ED(envelope.CodeActionFailed, "cannot write crontab", err.Error())
	}
	if err := tmp.Close(); err != nil {
		return 0, 0, envelope.ED(envelope.CodeActionFailed, "cannot close crontab", err.Error())
	}
	if err := os.Rename(tmpName, path); err != nil {
		return 0, 0, envelope.ED(envelope.CodeActionFailed, "cannot replace crontab", err.Error())
	}
	lines := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) != "" {
			lines++
		}
	}
	return len(content), lines, nil
}
