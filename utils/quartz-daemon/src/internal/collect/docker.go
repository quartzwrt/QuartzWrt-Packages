package collect

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"sort"
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
	running, total, unhealthy, containers := parseContainerDigest(raw)

	rawImg, err := dockerGet("/images/json")
	if err != nil {
		return nil, fmt.Errorf("docker: socket unavailable")
	}
	var images []jsonRaw
	if err := jsonUnmarshal(rawImg, &images); err != nil {
		return nil, fmt.Errorf("docker: images parse error")
	}
	out := &snapshot.Docker{
		Available:  true,
		Running:    running,
		Total:      total,
		Images:     len(images),
		Unhealthy:  unhealthy,
		Containers: containers,
	}
	// 以下为可选信息（2026-10-19 加法）：任一失败只省略对应字段，不影响计数主面。
	if rawInfo, err := dockerGet("/info"); err == nil {
		var info struct {
			ServerVersion string `json:"ServerVersion"`
			Driver        string `json:"Driver"`
			DockerRootDir string `json:"DockerRootDir"`
			NCPU          int    `json:"NCPU"`
			MemTotal      int64  `json:"MemTotal"`
		}
		if jsonUnmarshal(rawInfo, &info) == nil {
			out.Version, out.Driver, out.RootDir = info.ServerVersion, info.Driver, info.DockerRootDir
			out.CPUs, out.MemTotal = info.NCPU, info.MemTotal
		}
	}
	if rawNet, err := dockerGet("/networks"); err == nil {
		var nets []jsonRaw
		if jsonUnmarshal(rawNet, &nets) == nil {
			out.Networks = len(nets)
		}
	}
	if rawVol, err := dockerGet("/volumes"); err == nil {
		var vol struct {
			Volumes []jsonRaw `json:"Volumes"`
		}
		if jsonUnmarshal(rawVol, &vol) == nil {
			out.Volumes = len(vol.Volumes)
		}
	}
	return out, nil
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
	running, total, unhealthy, _ = parseContainerDigest(raw)
	return running, total, unhealthy
}

// parseContainerDigest 计数 + 不健康名 + 容器摘要（≤8；运行中优先，按名字排序）。
func parseContainerDigest(raw []byte) (running, total int, unhealthy []string, containers []snapshot.DockerContainer) {
	var cs []struct {
		Names  []string `json:"Names"`
		Image  string   `json:"Image"`
		State  string   `json:"State"`
		Status string   `json:"Status"`
	}
	unhealthy = []string{}
	if err := jsonUnmarshal(raw, &cs); err != nil {
		return 0, 0, unhealthy, nil
	}
	total = len(cs)
	containers = make([]snapshot.DockerContainer, 0, len(cs))
	for _, ct := range cs {
		name := ""
		if len(ct.Names) > 0 {
			name = strings.TrimPrefix(ct.Names[0], "/")
		}
		if ct.State == "running" {
			running++
		}
		if strings.Contains(ct.Status, "(unhealthy)") && len(unhealthy) < snapshot.MaxUnhealthy && name != "" {
			unhealthy = append(unhealthy, name)
		}
		if name != "" && len(containers) < snapshot.MaxDockerContainers {
			containers = append(containers, snapshot.DockerContainer{
				Name: name, Image: ct.Image, State: ct.State, Status: ct.Status,
			})
		}
	}
	sort.SliceStable(containers, func(i, j int) bool {
		ri := containers[i].State != "running"
		rj := containers[j].State != "running"
		if ri != rj {
			return !ri
		}
		return containers[i].Name < containers[j].Name
	})
	return running, total, unhealthy, containers
}
