package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// ubusCall 执行 `ubus call <obj> <method>` 并返回 JSON 输出（3s 超时）。
func ubusCall(obj, method string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ubus", "call", obj, method).Output()
	if err != nil {
		return nil, fmt.Errorf("ubus %s %s: %v", obj, method, err)
	}
	return out, nil
}

// ubusServiceStates 取 procd 服务运行态：name → 存在任一 running 实例。
func ubusServiceStates() (map[string]bool, error) {
	out, err := ubusCall("service", "list")
	if err != nil {
		return nil, fmt.Errorf("services: ubus unavailable")
	}
	var all map[string]struct {
		Instances map[string]struct {
			Running bool `json:"running"`
		} `json:"instances"`
	}
	if err := json.Unmarshal(out, &all); err != nil {
		return nil, fmt.Errorf("services: ubus output parse error")
	}
	st := map[string]bool{}
	for name, svc := range all {
		for _, inst := range svc.Instances {
			if inst.Running {
				st[name] = true
				break
			}
		}
	}
	return st, nil
}

// ubusWANProto 取 WAN 协议名（慢刷）：优先接口名 "wan"，否则按默认路由
// 设备匹配 l3_device。ubus/接口缺失（如 VM 无 WAN 段）→ 空串（合法态）。
func ubusWANProto(defaultIface string) string {
	out, err := ubusCall("network.interface", "dump")
	if err != nil {
		return ""
	}
	var d struct {
		Interface []struct {
			Interface string `json:"interface"`
			Proto     string `json:"proto"`
			L3Device  string `json:"l3_device"`
		} `json:"interface"`
	}
	if json.Unmarshal(out, &d) != nil {
		return ""
	}
	for _, i := range d.Interface {
		if i.Interface == "wan" && i.Proto != "" {
			return i.Proto
		}
	}
	if defaultIface != "" {
		for _, i := range d.Interface {
			if i.L3Device == defaultIface && i.Proto != "" && i.Proto != "none" {
				return i.Proto
			}
		}
	}
	return ""
}

// ubusBoardModel 取设备型号（/tmp/sysinfo/model 缺失时的兜底）。
func ubusBoardModel() string {
	out, err := ubusCall("system", "board")
	if err != nil {
		return ""
	}
	var b struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(out, &b) != nil {
		return ""
	}
	return b.Model
}
