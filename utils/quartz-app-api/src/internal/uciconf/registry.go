// 配置面注册表（试点切片：接口 / 无线 / DHCP·DNS / 系统）。
//
// 口径（IA v2 定案 D2′「按节完整注册」）：**每个 section 类型声明其全部可读写字段**，
// 不做散点键白名单——否则「有的字段能改有的不能」本身就是半成品。字段清单来源见
// `QuartzWrt-App/docs/IA-V2-DISCUSSION.md` §14（从 LuCI 自身定义提取）。
//
// 尚未注册（W6.11 及以后）：防火墙（zone/rule/redirect）、静态路由、管理权、
// 计划任务/挂载/LED、wireless 的 MAC 过滤与漫游段。未注册的读写一律 `forbidden_action`。
package uciconf

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/quartzwrt/quartz-app-api/internal/envelope"
)

// 值类型。
type valKind int

const (
	kStr       valKind = iota // 字符串（可带字符集/长度约束）
	kEnum                     // 枚举（allowed）
	kBool                     // 0/1
	kUint                     // 非负整数（min/max）
	kIPv4                     // IPv4 地址
	kIPv4Mask                 // IPv4 掩码
	kIPList                   // 空格分隔的 IP/主机名列表（DNS、NTP 等）
	kHostList                 // 主机名列表的别名（同 kIPList 校验）
	kLeaseTime                // dnsmasq 租期（"12h"/"30m"/"infinite"）
	kList                     // 字符串列表（多值）
)

// optionSpec 一个字段。
type optionSpec struct {
	Kind      valKind
	Allowed   []string
	Min       uint64
	Max       uint64
	MaxLen    int
	Pattern   *regexp.Regexp
	Sensitive bool // 读面掩码（wifi key / pppoe 口令）
}

// sectionSpec 一个 section 类型。
type sectionSpec struct {
	Type    string
	Options map[string]optionSpec
}

// configSpec 一个配置文件。
type configSpec struct {
	Name     string
	Sections map[string]sectionSpec // key = 节名字面量，或 "@<type>" 匹配任意索引
}

var (
	reHostname = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,62}$`)
	reSSID     = regexp.MustCompile(`^[\x20-\x7e]{1,32}$`)
	reKeyStr   = regexp.MustCompile(`^[\x20-\x7e]{8,63}$`)
	reCountry  = regexp.MustCompile(`^[A-Z]{2}$`)
	reChan     = regexp.MustCompile(`^(auto|[0-9]{1,4})$`)
	reShort    = regexp.MustCompile(`^[A-Za-z0-9_.:-]{0,64}$`)
	reLease    = regexp.MustCompile(`^([0-9]{1,6}[smhd]|infinite)$`)
	reIPAny    = regexp.MustCompile(`^[0-9a-fA-F:.%]{2,45}$`)
)

// 常用枚举。
var (
	encryptionAllowed = []string{"none", "psk", "psk2", "psk-mixed", "sae", "sae-mixed", "wpa", "wpa2", "wpa-mixed"}
	htmodeAllowed     = []string{"HT20", "HT40", "VHT20", "VHT40", "VHT80", "VHT160", "HE20", "HE40", "HE80", "HE160", "NOHT"}
	preferProto       = []string{"static", "dhcp", "none"}
)

var registry = []configSpec{
	{
		Name: "network",
		Sections: map[string]sectionSpec{
			"lan": {Type: "interface", Options: map[string]optionSpec{
				"proto":     {Kind: kEnum, Allowed: []string{"static", "dhcp", "none"}},
				"ipaddr":    {Kind: kIPv4},
				"netmask":   {Kind: kIPv4Mask},
				"gateway":   {Kind: kIPv4},
				"ip6addr":   {Kind: kIPList},
				"ip6assign": {Kind: kUint, Max: 64},
				"device":    {Kind: kStr, Pattern: reShort},
				"disabled":  {Kind: kBool},
			}},
			"wan": {Type: "interface", Options: map[string]optionSpec{
				"proto":    {Kind: kEnum, Allowed: []string{"dhcp", "static", "pppoe", "none"}},
				"ipaddr":   {Kind: kIPv4},
				"netmask":  {Kind: kIPv4Mask},
				"gateway":  {Kind: kIPv4},
				"dns":      {Kind: kIPList},
				"username": {Kind: kStr, Pattern: reShort},
				"password": {Kind: kStr, MaxLen: 128, Sensitive: true},
				"device":   {Kind: kStr, Pattern: reShort},
				"disabled": {Kind: kBool},
				"auto":     {Kind: kBool},
			}},
		},
	},
	{
		Name: "wireless",
		Sections: map[string]sectionSpec{
			"@wifi-device": {Type: "wifi-device", Options: map[string]optionSpec{
				"disabled":     {Kind: kBool},
				"channel":      {Kind: kStr, Pattern: reChan},
				"htmode":       {Kind: kEnum, Allowed: htmodeAllowed},
				"txpower":      {Kind: kUint, Max: 30},
				"country":      {Kind: kStr, Pattern: reCountry},
				"band":         {Kind: kEnum, Allowed: []string{"2g", "5g", "6g"}},
				"cell_density": {Kind: kEnum, Allowed: []string{"0", "1", "2", "3", "4"}},
				"distance":     {Kind: kUint, Max: 100000},
			}},
			"@wifi-iface": {Type: "wifi-iface", Options: map[string]optionSpec{
				"ssid":       {Kind: kStr, Pattern: reSSID},
				"encryption": {Kind: kEnum, Allowed: encryptionAllowed},
				"key":        {Kind: kStr, Pattern: reKeyStr, Sensitive: true},
				"hidden":     {Kind: kBool},
				"isolate":    {Kind: kBool},
				"disabled":   {Kind: kBool},
				"wmm":        {Kind: kBool},
				"network":    {Kind: kList},
				"mode":       {Kind: kEnum, Allowed: []string{"ap", "sta", "mesh", "adhoc", "monitor"}},
			}},
		},
	},
	{
		Name: "dhcp",
		Sections: map[string]sectionSpec{
			"@dnsmasq": {Type: "dnsmasq", Options: map[string]optionSpec{
				"domain":            {Kind: kStr, Pattern: reHostname},
				"local":             {Kind: kStr, MaxLen: 64},
				"domainneeded":      {Kind: kBool},
				"boguspriv":         {Kind: kBool},
				"authoritative":     {Kind: kBool},
				"localservice":      {Kind: kBool},
				"rebind_protection": {Kind: kBool},
				"expandhosts":       {Kind: kBool},
				"cachesize":         {Kind: kUint, Max: 100000},
				"logqueries":        {Kind: kBool},
			}},
			"@dhcp": {Type: "dhcp", Options: map[string]optionSpec{
				"start":         {Kind: kUint, Max: 65535},
				"limit":         {Kind: kUint, Max: 65535},
				"leasetime":     {Kind: kLeaseTime},
				"ignore":        {Kind: kBool},
				"dynamicdhcp":   {Kind: kBool},
				"force":         {Kind: kBool},
				"ra":            {Kind: kEnum, Allowed: []string{"disabled", "server", "relay", "hybrid"}},
				"ra_management": {Kind: kEnum, Allowed: []string{"0", "1", "2"}},
				"dhcpv6":        {Kind: kEnum, Allowed: []string{"disabled", "server", "relay", "hybrid"}},
				"master":        {Kind: kBool},
				"dns_service":   {Kind: kBool},
			}},
		},
	},
	{
		Name: "system",
		Sections: map[string]sectionSpec{
			"@system": {Type: "system", Options: map[string]optionSpec{
				"hostname":     {Kind: kStr, Pattern: reHostname},
				"description":  {Kind: kStr, MaxLen: 64},
				"timezone":     {Kind: kStr, MaxLen: 64},
				"zonename":     {Kind: kStr, MaxLen: 64},
				"log_size":     {Kind: kUint, Max: 4096},
				"log_proto":    {Kind: kEnum, Allowed: []string{"udp", "tcp"}},
				"conloglevel":  {Kind: kUint, Max: 8},
				"cronloglevel": {Kind: kUint, Max: 8},
				"ttylogin":     {Kind: kBool},
				"log_ip":       {Kind: kStr, Pattern: reIPAny, MaxLen: 64},
				"log_port":     {Kind: kUint, Max: 65535},
			}},
		},
	},
}

func lookupConfig(name string) (configSpec, bool) {
	for _, c := range registry {
		if c.Name == name {
			return c, true
		}
	}
	return configSpec{}, false
}

// lookupSection 按「节名或 @类型」匹配节规格；typ 为实际 uci 类型（可空）。
func lookupSection(spec configSpec, section, typ string) (sectionSpec, bool) {
	// 字面量节名（如 lan/wan）
	if sec, ok := spec.Sections[section]; ok {
		return sec, true
	}
	// @type：当 section 是匿名/命名节且类型匹配时
	for key, sec := range spec.Sections {
		if !strings.HasPrefix(key, "@") {
			continue
		}
		want := strings.TrimPrefix(key, "@")
		if typ == want {
			return sec, true
		}
		// section 形如 @wifi-iface[0] 且 typ 未知时按前缀匹配
		if typ == "" && strings.HasPrefix(section, "@"+want) {
			return sec, true
		}
	}
	return sectionSpec{}, false
}

func lookupOption(spec configSpec, section, option string) (optionSpec, bool) {
	// 未知类型时按两种节规格都试（@type 与字面量）；命中即用
	if sec, ok := spec.Sections[section]; ok {
		o, has := sec.Options[option]
		return o, has
	}
	for key, sec := range spec.Sections {
		if !strings.HasPrefix(key, "@") {
			continue
		}
		if strings.HasPrefix(section, "@"+strings.TrimPrefix(key, "@")) {
			o, has := sec.Options[option]
			return o, has
		}
	}
	return optionSpec{}, false
}

// validateValue 值校验（类型/枚举/范围/字符集）→ 归一后的 JSON 值。
func validateValue(o optionSpec, v any) (any, *envelope.Error) {
	bad := func(what string) (any, *envelope.Error) {
		return nil, envelope.ED(envelope.CodeBadRequest, what, fmt.Sprintf("%v", v))
	}
	switch o.Kind {
	case kBool:
		switch t := v.(type) {
		case bool:
			if t {
				return 1, nil
			}
			return 0, nil
		case string:
			if t == "1" || t == "true" || t == "0" || t == "false" {
				if t == "1" || t == "true" {
					return 1, nil
				}
				return 0, nil
			}
		}
		return bad("invalid bool")
	case kUint:
		var n uint64
		switch t := v.(type) {
		case float64:
			if t < 0 || t != float64(uint64(t)) {
				return bad("invalid integer")
			}
			n = uint64(t)
		case string:
			if _, err := fmt.Sscanf(t, "%d", &n); err != nil {
				return bad("invalid integer")
			}
		case int:
			n = uint64(t)
		default:
			return bad("invalid integer")
		}
		if n < o.Min || n > o.Max {
			return bad("out of range")
		}
		return json.Number(fmt.Sprintf("%d", n)), nil
	case kEnum:
		s, ok := v.(string)
		if !ok {
			return bad("invalid enum")
		}
		for _, a := range o.Allowed {
			if a == s {
				return s, nil
			}
		}
		return bad("value not allowed")
	case kIPv4:
		s, ok := v.(string)
		if !ok || s == "" {
			return bad("invalid IPv4")
		}
		if s != "0.0.0.0" && net.ParseIP(s) == nil {
			return bad("invalid IPv4")
		}
		return s, nil
	case kIPv4Mask:
		s, ok := v.(string)
		if !ok {
			return bad("invalid netmask")
		}
		ip := net.ParseIP(s)
		if ip == nil || ip.To4() == nil {
			return bad("invalid netmask")
		}
		return s, nil
	case kLeaseTime:
		s, ok := v.(string)
		if !ok || !reLease.MatchString(s) {
			return bad("invalid leasetime")
		}
		return s, nil
	case kIPList, kHostList:
		list, ee := toStringList(v, 16, 64)
		if ee != nil {
			return nil, ee
		}
		for _, item := range list {
			it, _ := item.(string)
			if !reIPAny.MatchString(it) && !reHostname.MatchString(it) {
				return bad("invalid list item")
			}
		}
		return list, nil
	case kList:
		list, ee := toStringList(v, 16, 64)
		if ee != nil {
			return nil, ee
		}
		for _, item := range list {
			if it, _ := item.(string); !reShort.MatchString(it) {
				return bad("invalid list item")
			}
		}
		return list, nil
	default: // kStr
		s, ok := v.(string)
		if !ok {
			return bad("invalid string")
		}
		if o.MaxLen > 0 && len(s) > o.MaxLen {
			return bad("string too long")
		}
		if o.Pattern != nil && !o.Pattern.MatchString(s) {
			return bad("string rejected by charset")
		}
		if s == "__masked__" {
			return bad("masked placeholder rejected")
		}
		return s, nil
	}
}

// toStringList 归一「列表入参」：字符串（空格分隔或 JSON 数组串）或数组 → []string。
func toStringList(v any, maxItems, maxLen int) ([]any, *envelope.Error) {
	var items []string
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if strings.HasPrefix(s, "[") {
			var arr []string
			if json.Unmarshal([]byte(s), &arr) == nil {
				items = append(items, arr...)
			} else {
				return nil, envelope.E(envelope.CodeBadRequest, "invalid list")
			}
		} else if s != "" {
			items = strings.Fields(s)
		}
	case []any:
		for _, it := range t {
			s, ok := it.(string)
			if !ok {
				return nil, envelope.E(envelope.CodeBadRequest, "invalid list item")
			}
			items = append(items, s)
		}
	default:
		return nil, envelope.E(envelope.CodeBadRequest, "invalid list")
	}
	if len(items) > maxItems {
		return nil, envelope.E(envelope.CodeBadRequest, "too many list items")
	}
	out := make([]any, 0, len(items))
	for _, it := range items {
		if len(it) > maxLen {
			return nil, envelope.E(envelope.CodeBadRequest, "list item too long")
		}
		out = append(out, it)
	}
	return out, nil
}
