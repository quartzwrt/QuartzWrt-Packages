package collect

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/quartzwrt/quartz-daemon/internal/snapshot"
)

const dockerSocket = "/var/run/docker.sock"

// dockerClient 原生 HTTP over unix socket（零第三方依赖，DESIGN §9-D1）。
var dockerClient = &http.Client{
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", dockerSocket)
		},
	},
	Timeout: 2 * time.Second,
}

// collectDocker 采集 Docker 状态（慢刷）：
//   - 未安装 dockerd → available=false（Dashboard 隐藏整卡，契约 §3.4）；
//     docker 为按需安装包（2026-10-12 移出预装）——出厂固件 available=false 是常态
//   - 已安装但 socket 不可用/未运行 → 整节 nil + errors（下轮自动重试）
//
// API 路径一律用**无版本**形式：锁定基座 dockerd 29.6.1 的默认最低接受
// API 已升至 1.40，写死 /v1.24 会被版本中间件拒 "client version ... too old"；
// 无版本请求按服务端当前版本接受（DESIGN v0.3.1 / WORKPLAN 坑位 #24 同期核实）。
func collectDocker() (*snapshot.Docker, error) {
	if _, err := exec.LookPath("dockerd"); err != nil {
		return &snapshot.Docker{Available: false, Unhealthy: []string{}}, nil
	}
	raw, err := dockerGet("/containers/json?all=1")
	if err != nil {
		return nil, fmt.Errorf("docker: socket unavailable")
	}
	running, total, unhealthy := parseContainers(raw)

	rawImg, err := dockerGet("/images/json")
	if err != nil {
		return nil, fmt.Errorf("docker: socket unavailable")
	}
	var images []jsonRaw
	if err := jsonUnmarshal(rawImg, &images); err != nil {
		return nil, fmt.Errorf("docker: images parse error")
	}
	return &snapshot.Docker{
		Available: true,
		Running:   running,
		Total:     total,
		Images:    len(images),
		Unhealthy: unhealthy,
	}, nil
}

func dockerGet(path string) ([]byte, error) {
	resp, err := dockerClient.Get("http://docker" + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// parseContainers 从 /containers/json 响应提取计数与不健康容器名（≤8 截断）。
func parseContainers(raw []byte) (running, total int, unhealthy []string) {
	var cs []struct {
		Names  []string `json:"Names"`
		State  string   `json:"State"`
		Status string   `json:"Status"`
	}
	unhealthy = []string{}
	if err := jsonUnmarshal(raw, &cs); err != nil {
		return 0, 0, unhealthy
	}
	total = len(cs)
	for _, ct := range cs {
		if ct.State == "running" {
			running++
		}
		if strings.Contains(ct.Status, "(unhealthy)") && len(unhealthy) < snapshot.MaxUnhealthy {
			name := ""
			if len(ct.Names) > 0 {
				name = strings.TrimPrefix(ct.Names[0], "/")
			}
			if name != "" {
				unhealthy = append(unhealthy, name)
			}
		}
	}
	return running, total, unhealthy
}
