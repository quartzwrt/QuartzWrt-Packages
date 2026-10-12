package collect

import (
	"context"
	"math"
	"os"
	"os/exec"
	"strings"
	"time"
)

// readFile 读取文件（失败返回 nil，调用方按缺省处理）。
func readFile(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return b
}

// readTrim 读取文件并去除首尾空白。
func readTrim(path string) string {
	return strings.TrimSpace(string(readFile(path)))
}

// execTimeout 执行命令（含超时）并返回合并输出。
func execTimeout(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// round1 保留一位小数。
func round1(v float64) float64 { return math.Round(v*10) / 10 }

// pct 由 used/total 计算百分比（1 位小数；total≤0 → 0）。
func pct(used, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return round1(float64(used) / float64(total) * 100)
}

// unquote 去掉 shell 风格单/双引号包裹。
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// ring 定长环形缓冲（保留最近 max 个值）。
type ring struct {
	buf []int64
	max int
}

func newRing(max int) *ring { return &ring{max: max} }

func (r *ring) push(v int64) {
	r.buf = append(r.buf, v)
	if len(r.buf) > r.max {
		r.buf = r.buf[len(r.buf)-r.max:]
	}
}

func (r *ring) slice() []int64 { return append([]int64{}, r.buf...) }
