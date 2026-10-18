// Package ubusfwd ubus 转发项机制（WP-5；DESIGN §5「ubus 转发项」落地，2026-10-17；
// 2026-10-18 扩展：qmodem 蜂窝域读写条目）。
//
// 纪律（与 actions / pkgs 包同款）：**无任意透传**——目标 (object, method) 与
// 参数模式**逐项显式注册**（表驱动），参数只做结构 + 字符集/数值域校验，绝不
// 拼接 shell；执行 = `ubus call <object> <method> <json>`（JSON 作为单个 argv）。
//
// 条目来源：
//
//	bandix.*  → quartz.bandix（WP-8 bridge 契约；数值字段收 data 对象）
//	qmodem.*  → qmodem（WP-7 收录的第三方插件 rpcd 接口；与网页端同源；
//	            W6.12 蜂窝轮，见 QuartzWrt-App/docs/W6.12-CELLULAR-DESIGN.md §3）
//
// **入参形态（shape）**：每项声明上游要哪一种 JSON 形态，转发时按声明重建：
//
//	shapeFlat          参数平铺（含 config_section，若该条目声明）
//	shapeData          参数收进 data 对象（bridge 数值方法形态，WP-8 坑位 #61）
//	shapeSectionParams config_section 置于顶层 + 其余收进 params 对象/数组
//	shapeSectionOnly   仅 config_section（拨号/清理类）
//	shapeNone          无参（tty_ports 等）
//
// **厂商方言参数（第二档校验）**：`set_lockband` 的 `{band_class, lock_band}`、
// `set_network_prefer` 的 `["4G","5G"]` 数组等，形态由上游厂商脚本定义（**与其
// rpcd 文档示例不一致**——已按源码核实）。这类参数取**结构校验**（对象/数组
// 形状 + 键名白名单 + 值类型与长度上限），语义交上游判定；其余参数（section /
// at / imei / phone / 索引列表 / 枚举）取**严格字段校验**。
//
// **读面例外（2026-10-18 增补，先例 bandix.limits）**：快照承载概览级只读摘要；
// **页面级读**（AT 端口与预置命令、短信列表、设置回显、能力声明）走本机制——
// 条目仍逐条显式注册，无自由参数。
//
// **上游应答两形态**：bridge 走信封 `{ok,data|error}`；qmodem 插件直接输出结果
// 对象（无 ok 字段）→ 视为成功，整对象作 data；其错误以 `{"error":"…"}` 出现。
//
// **扩展**：新条目 = registry 加一行 + 测试；新对象（如 ddns bridge）同样只加行
// （ID 前缀路由由 Handles 判定），机制不变。
package ubusfwd

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/quartzwrt/quartz-app-api/internal/audit"
	"github.com/quartzwrt/quartz-app-api/internal/envelope"
)

// Timeout 单次转发上限（bridge/插件端均为本地 ubus 调用，5s 富余）。
const Timeout = 5 * time.Second

// maxRate bridge 契约数值上限（字节数/Byte/s，0…2^40 = 1 TiB）。
const maxRate = uint64(1) << 40

// maxTextLen 文本类参数上限（短信内容等）。
const maxTextLen = 480

// Runner 执行器（注入测试用）。返回 stdout（ubus call 的 JSON 输出）与退出错误。
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

var (
	macRe       = regexp.MustCompile(`^[0-9a-fA-F]{2}(:[0-9a-fA-F]{2}){5}$`)
	hostnameRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)
	digitsRe    = regexp.MustCompile(`^[0-9]{1,20}$`)
	sectionRe   = regexp.MustCompile(`^[A-Za-z0-9_]{1,32}$`)
	atRe        = regexp.MustCompile("^AT[+*]?[A-Za-z0-9_=,.:\"?#$%&'()*/<>@\\[\\]{}~\\- ]{0,240}$")
	portRe      = regexp.MustCompile(`^/dev/[A-Za-z0-9._/-]{1,64}$`)
	phoneRe     = regexp.MustCompile(`^\+?[0-9]{3,20}$`)
	indexListRe = regexp.MustCompile(`^[0-9]{1,3}( [0-9]{1,3}){0,19}$`)
	labelRe     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,15}$`)
	initNameRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
)

// 参数类型。
type kind int

const (
	kindMAC     kind = iota // MAC（归一化为小写）
	kindUint                // 十进制整数 + 域
	kindHost                // 主机名（非空）
	kindHostOpt             // 主机名（允许空串 = 清除绑定）
	// —— 2026-10-18（qmodem 蜂窝域）——
	kindSection   // uci qmodem modem-device section（白名单，注入 provider）
	kindEnum      // 枚举（field.allowed）
	kindLabel     // 短标识（qmi/ecm/mbim… 或 SM/ME/频段列表；字符集 + 长度）
	kindAT        // AT 指令（字符集 + 长度）
	kindPort      // AT 端口路径（白名单优先，回退字符集）
	kindPhone     // 电话号码
	kindText      // 文本（长度上限 + 拒绝控制字符；允许换行/制表，SMS 用）
	kindPassword  // 密码（长度上限 + 拒绝所有控制字符，含换行/制表）
	kindDigits    // 定长数字（IMEI 15 位等）
	kindIndexList // 空格分隔的数字索引列表（delete_sms）
	kindJSONObj   // 厂商方言对象参数（field.sub 子模式）
	kindJSONArr   // 厂商方言数组参数（元素 ∈ field.allowed）
	kindInitName  // procd init 脚本名（/etc/init.d/<name>；字符集 + 长度）
)

type field struct {
	kind     kind
	min      uint64
	max      uint64
	allowed  []string
	minLen   int
	maxLen   int
	optional bool             // 缺省即不传（如 at_cfg 的 lang）
	sub      map[string]field // kindJSONObj 的子模式
}

// 上游 JSON 形态。
type shape int

const (
	shapeFlat shape = iota
	shapeData
	shapeSectionParams
	shapeSectionOnly
	shapeNone
)

// entry 一条转发项。
type entry struct {
	id     string // action id（App 侧）
	object string // ubus 对象（固定）
	method string // 上游方法（固定）
	shape  shape
	// sectionKey 上游的模组标识参数名（默认 config_section；`qmodem.sms` 对象用
	// modem_id）——App 统一以 `config_section` 传参，转发时按此名重建。
	sectionKey string
	fields     map[string]field
}

var registry = []entry{
	// ---------------- bandix（WP-8 bridge 契约） ----------------
	{
		id: "bandix.limit_default_set", object: "quartz.bandix", method: "limit_default_set", shape: shapeData,
		fields: map[string]field{
			"wan_rx_rate_limit": {kind: kindUint, max: maxRate},
			"wan_tx_rate_limit": {kind: kindUint, max: maxRate},
		},
	},
	{
		id: "bandix.whitelist_add", object: "quartz.bandix", method: "whitelist_add",
		fields: map[string]field{"mac": {kind: kindMAC}},
	},
	{
		id: "bandix.whitelist_delete", object: "quartz.bandix", method: "whitelist_delete",
		fields: map[string]field{"mac": {kind: kindMAC}},
	},
	{
		id: "bandix.whitelist_enabled", object: "quartz.bandix", method: "whitelist_enabled", shape: shapeData,
		fields: map[string]field{"enabled": {kind: kindUint, max: 1}},
	},
	{
		id: "bandix.hostname_set", object: "quartz.bandix", method: "hostname_set",
		fields: map[string]field{"mac": {kind: kindMAC}, "hostname": {kind: kindHostOpt}},
	},
	{
		// 唯一读条目：限速/白名单/配额聚合（无参）——App 的「设置类对话框」取当前值
		// （该状态不在 daemon 快照内）；状态/流量类读面一律走快照，见包注释。
		id: "bandix.limits", object: "quartz.bandix", method: "limits", shape: shapeNone,
		fields: map[string]field{},
	},

	// ---------------- qmodem · 写面（W6.12 蜂窝轮） ----------------
	{
		// 拨号控制：断开会使模组数据通道下线（危险动作，App 侧确认层负责）。
		id: "qmodem.dial", object: "qmodem", method: "modem_dial", shape: shapeSectionOnly,
		fields: map[string]field{"config_section": {kind: kindSection}},
	},
	{
		id: "qmodem.hang", object: "qmodem", method: "modem_hang", shape: shapeSectionOnly,
		fields: map[string]field{"config_section": {kind: kindSection}},
	},
	{
		id: "qmodem.redial", object: "qmodem", method: "modem_redial", shape: shapeSectionOnly,
		fields: map[string]field{"config_section": {kind: kindSection}},
	},
	{
		// **USB 拨号模式**（AT+QCFG="usbnet"：qmi/ecm/mbim/rndis/ncm）——注意不是
		// 「5G/4G 网络模式」（那是 set_network_prefer）；改它会重新枚举 USB 数据
		// 通道 → 可能当场断网，危险动作。
		id: "qmodem.set_usb_mode", object: "qmodem", method: "set_mode",
		fields: map[string]field{
			"config_section": {kind: kindSection},
			"mode":           {kind: kindEnum, allowed: []string{"qmi", "ecm", "mbim", "rndis", "ncm"}},
		},
	},
	{
		// 频段锁定（厂商方言参数，结构校验）：Quectel/Fibocom 方言
		// `{"band_class":"NR","lock_band":"41,78,79"}`。危险动作（锁错频段会失联）。
		id: "qmodem.set_lockband", object: "qmodem", method: "set_lockband", shape: shapeSectionParams,
		fields: map[string]field{
			"config_section": {kind: kindSection},
			"params": {kind: kindJSONObj, sub: map[string]field{
				"band_class": {kind: kindEnum, allowed: []string{"LTE", "NR"}},
				"lock_band":  {kind: kindLabel, maxLen: 64},
			}},
		},
	},
	{
		// 网络偏好（厂商方言参数，结构校验）：上游实现接收**字符串数组**
		// （`contains(["4G","5G"])`）——与其 rpcd 文档示例 `{"prefer":"lte"}` 不一致，
		// 按源码实现为准。
		id: "qmodem.set_network_prefer", object: "qmodem", method: "set_network_prefer", shape: shapeSectionParams,
		fields: map[string]field{
			"config_section": {kind: kindSection},
			"params":         {kind: kindJSONArr, allowed: []string{"3G", "4G", "5G"}},
		},
	},
	{
		// AT 调试：全命令面（IA v2 定案 D4′——命令面不缩水，风险由用户自担，
		// 界面二次确认 + 风险警告 + 本审计）；此处只做字符集/长度校验。
		id: "qmodem.send_at", object: "qmodem", method: "send_at", shape: shapeSectionParams,
		fields: map[string]field{
			"config_section": {kind: kindSection},
			"params": {kind: kindJSONObj, sub: map[string]field{
				"at":   {kind: kindAT},
				"port": {kind: kindPort},
			}},
		},
	},
	{
		id: "qmodem.send_sms", object: "qmodem", method: "send_sms", shape: shapeSectionParams,
		fields: map[string]field{
			"config_section": {kind: kindSection},
			"params": {kind: kindJSONObj, sub: map[string]field{
				"phone_number":    {kind: kindPhone},
				"message_content": {kind: kindText, maxLen: maxTextLen},
			}},
		},
	},
	{
		id: "qmodem.delete_sms", object: "qmodem", method: "delete_sms",
		fields: map[string]field{
			"config_section": {kind: kindSection},
			"index":          {kind: kindIndexList},
		},
	},
	{
		id: "qmodem.set_sms_storage", object: "qmodem", method: "set_sms_storage",
		fields: map[string]field{
			"config_section": {kind: kindSection},
			"storage":        {kind: kindEnum, allowed: []string{"SM", "ME", "MT"}},
		},
	},
	{
		id: "qmodem.set_traffic_reset_schedule", object: "qmodem", method: "set_traffic_reset_schedule",
		fields: map[string]field{
			"config_section": {kind: kindSection},
			"enabled":        {kind: kindUint, max: 1},
			"reset_type":     {kind: kindEnum, allowed: []string{"daily", "monthly"}},
			"hour":           {kind: kindUint, max: 23},
			"day":            {kind: kindUint, max: 28},
		},
	},
	{
		id: "qmodem.clear_stats", object: "qmodem", method: "clear_stats", shape: shapeSectionOnly,
		fields: map[string]field{"config_section": {kind: kindSection}},
	},
	{
		id: "qmodem.save_stats", object: "qmodem", method: "save_stats", shape: shapeSectionOnly,
		fields: map[string]field{"config_section": {kind: kindSection}},
	},
	{
		id: "qmodem.clear_dial_log", object: "qmodem", method: "clear_dial_log", shape: shapeSectionOnly,
		fields: map[string]field{"config_section": {kind: kindSection}},
	},
	{
		// 模组重启（软/硬，能力由 qmodem.reboot_caps 声明；危险动作）。
		id: "qmodem.reboot", object: "qmodem", method: "do_reboot", shape: shapeSectionParams,
		fields: map[string]field{
			"config_section": {kind: kindSection},
			"params": {kind: kindJSONObj, sub: map[string]field{
				"method": {kind: kindEnum, allowed: []string{"soft", "hard"}},
			}},
		},
	},
	{
		// SIM 槽位切换（能力由 qmodem.sim_switch_capabilities 声明；危险：会断网）。
		id: "qmodem.set_sim_slot", object: "qmodem", method: "set_sim_slot",
		fields: map[string]field{
			"config_section": {kind: kindSection},
			"slot":           {kind: kindEnum, allowed: []string{"1", "2"}},
		},
	},
	{
		// IMEI（敏感；IA v2 定案 D4′：命令面不缩水，界面风险警告 + 二次确认 + 审计）。
		id: "qmodem.set_imei", object: "qmodem", method: "set_imei",
		fields: map[string]field{
			"config_section": {kind: kindSection},
			"imei":           {kind: kindDigits, minLen: 15, maxLen: 15},
		},
	},
	{
		// 短信发送（独立对象；参数名 recipient/content）
		id: "qmodem.sms_send", object: "qmodem.sms", method: "send", sectionKey: "modem_id",
		fields: map[string]field{
			"config_section": {kind: kindSection},
			"recipient":      {kind: kindPhone},
			"content":        {kind: kindText, maxLen: maxTextLen},
		},
	},
	{
		// 短信删除（id + index 由列表返回；两者都要求，与网页端调用一致）
		id: "qmodem.sms_delete", object: "qmodem.sms", method: "delete", sectionKey: "modem_id",
		fields: map[string]field{
			"config_section": {kind: kindSection},
			"id":             {kind: kindIndexList},
			"index":          {kind: kindIndexList},
		},
	},
	{
		// 标记已读
		id: "qmodem.sms_mark_read", object: "qmodem.sms", method: "mark_read", sectionKey: "modem_id",
		fields: map[string]field{
			"config_section": {kind: kindSection},
			"id":             {kind: kindIndexList},
		},
	},
	{
		// 存储位置（mem1/2/3；枚举 SM/ME/MT）
		id: "qmodem.sms_storage_set", object: "qmodem.sms", method: "storage_set", sectionKey: "modem_id",
		fields: map[string]field{
			"config_section": {kind: kindSection},
			"mem1":           {kind: kindEnum, allowed: []string{"SM", "ME", "MT"}},
			"mem2":           {kind: kindEnum, allowed: []string{"SM", "ME", "MT"}, optional: true},
			"mem3":           {kind: kindEnum, allowed: []string{"SM", "ME", "MT"}, optional: true},
		},
	},

	// ---------------- qmodem · 读面（页面级，W6.12 §3.2） ----------------
	{
		// AT 端口 + 预置命令（与网页端同一份 `get_at_cfg.cmds`）。
		// `lang`（可选，zh|en）= 取 App 界面语言 → 上游受控 fork 的 quartz 分支支持
		// 该参数（v3.4.0_rc3-quartz2；缺省行为不变，网页端不受影响）。
		id: "qmodem.at_cfg", object: "qmodem", method: "get_at_cfg", shape: shapeSectionOnly,
		fields: map[string]field{
			"config_section": {kind: kindSection},
			"lang":           {kind: kindEnum, allowed: []string{"zh", "en"}, optional: true},
		},
	},
	// —— 短信走**独立对象** `qmodem.sms`（与网页端同一数据源；`qmodem.get_sms` 是
	// 低层 tom_modem 原始输出，不含 is_read/id，不用）。注意其模组参数名为 `modem_id`。
	{
		// 短信列表（结构化：{messages:[{id,index,type,sender,recipient,content,timestamp,is_read}]}）
		id: "qmodem.sms_list", object: "qmodem.sms", method: "list", sectionKey: "modem_id",
		fields: map[string]field{
			"config_section": {kind: kindSection},
			"limit":          {kind: kindUint, min: 1, max: 500, optional: true},
			"offset":         {kind: kindUint, max: 100000, optional: true},
		},
	},
	{
		// 存储容量与当前配置（{configured, SM:{used,total}, ME:{...}}）
		id: "qmodem.sms_storage_get", object: "qmodem.sms", method: "storage_get", sectionKey: "modem_id",
		fields: map[string]field{"config_section": {kind: kindSection}},
	},
	{
		// 触发一次模组同步（不等上游 10s 缓存）
		id: "qmodem.sms_sync", object: "qmodem.sms", method: "sync", sectionKey: "modem_id",
		fields: map[string]field{"config_section": {kind: kindSection}},
	},
	{
		id: "qmodem.dial_log", object: "qmodem", method: "get_dial_log", shape: shapeSectionOnly,
		fields: map[string]field{"config_section": {kind: kindSection}},
	},
	{
		id: "qmodem.lockband", object: "qmodem", method: "get_lockband", shape: shapeSectionOnly,
		fields: map[string]field{"config_section": {kind: kindSection}},
	},
	{
		id: "qmodem.usb_mode", object: "qmodem", method: "get_mode", shape: shapeSectionOnly,
		fields: map[string]field{"config_section": {kind: kindSection}},
	},
	{
		id: "qmodem.network_prefer", object: "qmodem", method: "get_network_prefer", shape: shapeSectionOnly,
		fields: map[string]field{"config_section": {kind: kindSection}},
	},
	{
		id: "qmodem.neighborcell", object: "qmodem", method: "get_neighborcell", shape: shapeSectionOnly,
		fields: map[string]field{"config_section": {kind: kindSection}},
	},
	{
		id: "qmodem.current_band", object: "qmodem", method: "get_current_band", shape: shapeSectionOnly,
		fields: map[string]field{"config_section": {kind: kindSection}},
	},
	{
		id: "qmodem.traffic_reset_schedule", object: "qmodem", method: "get_traffic_reset_schedule", shape: shapeSectionOnly,
		fields: map[string]field{"config_section": {kind: kindSection}},
	},
	{
		id: "qmodem.sim_slot", object: "qmodem", method: "get_sim_slot", shape: shapeSectionOnly,
		fields: map[string]field{"config_section": {kind: kindSection}},
	},
	{
		id: "qmodem.sim_switch_capabilities", object: "qmodem", method: "get_sim_switch_capabilities", shape: shapeSectionOnly,
		fields: map[string]field{"config_section": {kind: kindSection}},
	},
	{
		id: "qmodem.reboot_caps", object: "qmodem", method: "get_reboot_caps", shape: shapeSectionOnly,
		fields: map[string]field{"config_section": {kind: kindSection}},
	},
	{
		id: "qmodem.disabled_features", object: "qmodem", method: "get_disabled_features", shape: shapeSectionOnly,
		fields: map[string]field{"config_section": {kind: kindSection}},
	},
	{
		// AT 端口列表（无参；App 的 AT 页端口下拉数据源）
		id: "qmodem.tty_ports", object: "qmodem", method: "get_tty_ports", shape: shapeNone,
		fields: map[string]field{},
	},

	// ---------------- luci（路由器 tab：启动项 / 时区，W6.10） ----------------
	{
		// 启动项列表（无参）：{name:{index,stop,enabled}}；`stop` 可为 null。
		id: "luci.init_list", object: "luci", method: "getInitList", shape: shapeNone,
		fields: map[string]field{},
	},
	{
		// 启动项动作（与网页端启动项页同源）：enable/disable 改 rc.d 链接，
		// start/stop/restart 调 init 脚本。`name` 必须是 /etc/init.d 下的脚本名，
		// 严格字符集校验（该值由上游拼进 init 调用，禁止任何路径分隔符）。
		id: "luci.init_action", object: "luci", method: "setInitAction",
		fields: map[string]field{
			"name":   {kind: kindInitName},
			"action": {kind: kindEnum, allowed: []string{"enable", "disable", "start", "stop", "restart", "reload"}},
		},
	},
	{
		// 时区列表（无参）：{zone:{tzstring,active?}}；App 系统页的时区选择器数据源。
		id: "luci.timezones", object: "luci", method: "getTimezones", shape: shapeNone,
		fields: map[string]field{},
	},
	{
		// 管理权·改密：上游 luci.setPassword 经 busybox passwd 写 /etc/shadow。
		// 只允许 root；密码长度上限 128、拒绝控制字符；审计只记录动作与目标，不记录密码。
		id: "luci.set_password", object: "luci", method: "setPassword",
		fields: map[string]field{
			"username": {kind: kindEnum, allowed: []string{"root"}},
			"password": {kind: kindPassword, maxLen: 128},
		},
	},
	{
		// 当前挂载点（/proc/mounts + statvfs）：[{device,mount,size,avail,free}]。
		id: "luci.mounts", object: "luci", method: "getMountPoints", shape: shapeNone,
		fields: map[string]field{},
	},
	{
		// 块设备（block info + /proc/swaps）：{dev:{dev,size,type,...}}。
		id: "luci.block_devices", object: "luci", method: "getBlockDevices", shape: shapeNone,
		fields: map[string]field{},
	},
	{
		// 物理 LED 列表（sysfs）：{led:{triggers[],active_trigger,brightness,max_brightness}}。
		id: "luci.leds", object: "luci", method: "getLEDs", shape: shapeNone,
		fields: map[string]field{},
	},
}

// Handles 判定 action id 是否由本机制承载（main.go 前缀路由用）。
func Handles(id string) bool {
	for _, e := range registry {
		if e.id == id {
			return true
		}
	}
	return false
}

// Manager 转发执行器。
type Manager struct {
	Run Runner // 默认 exec 直执行
	// Sections 已知模组 section 白名单（kindSection 用；nil = 不可用）。
	Sections func() ([]string, error)
	// Ports 某模组可用 AT 端口（kindPort 用；nil 或返回空 = 只做路径字符集校验）。
	Ports func(section string) ([]string, error)
}

// New 构造（生产默认：section 读 uci，端口读 qmodem get_tty_ports）。
func New() *Manager {
	return &Manager{Run: execRun, Sections: uciModemSections, Ports: ubusTTYPorts}
}

// Execute 校验并转发一条动作；一切尝试（含失败）落 logd 审计。
func (m *Manager) Execute(ctx context.Context, id string, args map[string]string) (map[string]any, *envelope.Error) {
	var ent *entry
	for i := range registry {
		if registry[i].id == id {
			ent = &registry[i]
			break
		}
	}
	if ent == nil {
		return nil, envelope.ED(envelope.CodeForbiddenAction, "unknown action", id)
	}

	out, ee := m.validate(ent, args)
	if ee != nil {
		audit.Logf("fwd action=%s fail reason=invalid", id)
		return nil, ee
	}
	body, hasBody, err := buildPayload(ent, out)
	if err != nil {
		return nil, envelope.E(envelope.CodeInternal, "marshal failed")
	}

	argv := []string{"call", ent.object, ent.method}
	if hasBody {
		argv = append(argv, body)
	}

	start := time.Now()
	cctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	raw, err := m.Run(cctx, "ubus", argv...)
	dur := time.Since(start)
	if err != nil {
		audit.Logf("fwd action=%s target=%s.%s fail dur_ms=%d reason=exec", id, ent.object, ent.method, dur.Milliseconds())
		return nil, envelope.ED(envelope.CodeDataUnavailable, "upstream unavailable", strings.TrimSpace(string(raw)))
	}

	// 上游应答两形态：
	//   ① bridge 信封 `{ok:bool, data|error:{code,message}}`；
	//   ② qmodem 插件裸对象（无 ok 字段）——成功即整对象作 data，错误为
	//      `{"error":"<字符串>"}`（如 "unknown modem section"）。
	var probe struct {
		OK    *bool           `json:"ok"`
		Data  json.RawMessage `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		audit.Logf("fwd action=%s fail dur_ms=%d reason=parse", id, dur.Milliseconds())
		return nil, envelope.ED(envelope.CodeActionFailed, "bad upstream response", truncate(string(raw), 200))
	}
	var data any
	if probe.OK != nil {
		if !*probe.OK {
			msg, code := "upstream error", envelope.CodeActionFailed
			var eo struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if len(probe.Error) > 0 && json.Unmarshal(probe.Error, &eo) == nil {
				if eo.Message != "" {
					msg = eo.Message
				}
				switch eo.Code {
				case "bad_request":
					code = envelope.CodeBadRequest
				case "data_unavailable":
					code = envelope.CodeDataUnavailable
				}
			}
			audit.Logf("fwd action=%s target=%s.%s fail dur_ms=%d code=%s", id, ent.object, ent.method, dur.Milliseconds(), code)
			return nil, envelope.ED(code, msg, "")
		}
		if len(probe.Data) > 0 {
			_ = json.Unmarshal(probe.Data, &data)
		}
	} else {
		// 裸对象：先看错误，再整体作 data
		var errStr string
		if len(probe.Error) > 0 && json.Unmarshal(probe.Error, &errStr) == nil && errStr != "" {
			audit.Logf("fwd action=%s target=%s.%s fail dur_ms=%d reason=upstream_error", id, ent.object, ent.method, dur.Milliseconds())
			return nil, envelope.ED(envelope.CodeActionFailed, errStr, "")
		}
		var obj map[string]any
		_ = json.Unmarshal(raw, &obj)
		data = obj
	}
	audit.Logf("fwd action=%s target=%s.%s ok dur_ms=%d", id, ent.object, ent.method, dur.Milliseconds())
	return map[string]any{
		"action": id,
		"result": map[string]any{"data": data},
	}, nil
}

// buildPayload 按条目声明的形态重建上游 JSON（返回 hasBody=false 表示不带 JSON argv）。
func buildPayload(ent *entry, vals map[string]any) (body string, hasBody bool, err error) {
	switch ent.shape {
	case shapeNone:
		return "", false, nil
	case shapeData:
		b, e := json.Marshal(map[string]any{"data": vals})
		return string(b), true, e
	case shapeSectionParams:
		payload := map[string]any{}
		rest := map[string]any{}
		key := ent.sectionKey
		if key == "" {
			key = "config_section"
		}
		for k, v := range vals {
			if k == "config_section" {
				payload[key] = v
				continue
			}
			rest[k] = v
		}
		// 单字段 `params`（对象/数组）直接作为 params；否则其余字段整体收进 params
		if p, ok := rest["params"]; ok && len(rest) == 1 {
			payload["params"] = p
		} else if len(rest) > 0 {
			payload["params"] = rest
		}
		b, e := json.Marshal(payload)
		return string(b), true, e
	default: // shapeFlat / shapeSectionOnly
		if ent.sectionKey != "" && ent.sectionKey != "config_section" {
			out := map[string]any{}
			for k, v := range vals {
				if k == "config_section" {
					out[ent.sectionKey] = v
					continue
				}
				out[k] = v
			}
			b, e := json.Marshal(out)
			return string(b), true, e
		}
		b, e := json.Marshal(vals)
		return string(b), true, e
	}
}

// validate 逐项校验参数（未声明键一律拒绝；缺必填/越界/字符集不符 → 错误）。
// config_section 先行校验：kindPort 需要它来取端口白名单（避免依赖 map 迭代顺序）。
func (m *Manager) validate(ent *entry, args map[string]string) (map[string]any, *envelope.Error) {
	for k := range args {
		if _, ok := ent.fields[k]; !ok {
			return nil, envelope.ED(envelope.CodeForbiddenAction, "unknown argument", k)
		}
	}
	out := map[string]any{}
	section := ""
	if spec, ok := ent.fields["config_section"]; ok {
		v, has := args["config_section"]
		if !has {
			return nil, envelope.ED(envelope.CodeBadRequest, "missing argument", "config_section")
		}
		val, ee := m.validateValue("config_section", spec, v, "")
		if ee != nil {
			return nil, ee
		}
		out["config_section"] = val
		section, _ = val.(string)
	}
	for name, spec := range ent.fields {
		if name == "config_section" {
			continue
		}
		v, ok := args[name]
		if !ok {
			if spec.optional {
				continue
			}
			return nil, envelope.ED(envelope.CodeBadRequest, "missing argument", name)
		}
		val, ee := m.validateValue(name, spec, v, section)
		if ee != nil {
			return nil, ee
		}
		out[name] = val
	}
	return out, nil
}

// validateValue 单值校验（kind → 校验器）；section 供 kindPort 查端口白名单。
func (m *Manager) validateValue(name string, spec field, v string, section string) (any, *envelope.Error) {
	bad := func(what string) (any, *envelope.Error) {
		return nil, envelope.ED(envelope.CodeBadRequest, what, name+": "+truncate(v, 60))
	}
	switch spec.kind {
	case kindMAC:
		if !macRe.MatchString(v) {
			return bad("invalid mac")
		}
		return strings.ToLower(v), nil
	case kindUint:
		if !digitsRe.MatchString(v) {
			return bad("invalid integer")
		}
		var n uint64
		for _, c := range []byte(v) {
			n = n*10 + uint64(c-'0')
			if n > 1<<62 {
				break
			}
		}
		if n < spec.min || n > spec.max {
			return bad("out of range")
		}
		return json.Number(v), nil
	case kindHost, kindHostOpt:
		if v == "" {
			if spec.kind == kindHostOpt {
				return "", nil
			}
			return bad("invalid hostname")
		}
		if !hostnameRe.MatchString(v) {
			return bad("invalid hostname")
		}
		return v, nil
	case kindSection:
		if !sectionRe.MatchString(v) {
			return bad("invalid config_section")
		}
		if m.Sections == nil {
			return nil, envelope.E(envelope.CodeDataUnavailable, "qmodem unavailable")
		}
		known, err := m.Sections()
		if err != nil || len(known) == 0 {
			return nil, envelope.E(envelope.CodeDataUnavailable, "qmodem unavailable")
		}
		for _, s := range known {
			if s == v {
				return v, nil
			}
		}
		return bad("unknown modem section")
	case kindEnum:
		for _, a := range spec.allowed {
			if a == v {
				return v, nil
			}
		}
		return bad("value not allowed")
	case kindLabel:
		max := spec.maxLen
		if max == 0 {
			max = 16
		}
		if len(v) > max || !labelLike(v) {
			return bad("invalid value")
		}
		return v, nil
	case kindAT:
		if !atRe.MatchString(v) {
			return bad("invalid AT command")
		}
		return v, nil
	case kindPort:
		if !portRe.MatchString(v) {
			return bad("invalid port")
		}
		if m.Ports != nil && section != "" {
			if list, err := m.Ports(section); err == nil && len(list) > 0 {
				for _, p := range list {
					if p == v {
						return v, nil
					}
				}
				return bad("port not available on this modem")
			}
		}
		return v, nil
	case kindPhone:
		if !phoneRe.MatchString(v) {
			return bad("invalid phone number")
		}
		return v, nil
	case kindText:
		max := spec.maxLen
		if max == 0 || max > maxTextLen {
			max = maxTextLen
		}
		if v == "" || len([]rune(v)) > max || hasControlChars(v) {
			return bad("invalid text")
		}
		return v, nil
	case kindPassword:
		max := spec.maxLen
		if max == 0 || max > 128 {
			max = 128
		}
		if v == "" || len([]rune(v)) > max {
			return bad("invalid password")
		}
		for _, r := range v {
			if r < 0x20 || r == 0x7f {
				return bad("invalid password")
			}
		}
		return v, nil
	case kindDigits:
		minL, maxL := spec.minLen, spec.maxLen
		if minL == 0 {
			minL = maxL
		}
		if len(v) < minL || len(v) > maxL {
			return bad("invalid length")
		}
		for i := 0; i < len(v); i++ {
			if v[i] < '0' || v[i] > '9' {
				return bad("invalid digits")
			}
		}
		return v, nil
	case kindIndexList:
		if !indexListRe.MatchString(v) {
			return bad("invalid index list")
		}
		return v, nil
	case kindInitName:
		if !initNameRe.MatchString(v) || strings.Contains(v, "..") {
			return bad("invalid init script name")
		}
		return v, nil
	case kindJSONObj:
		var obj map[string]json.RawMessage
		if json.Unmarshal([]byte(v), &obj) != nil || len(obj) == 0 {
			return bad("invalid params object")
		}
		res := map[string]any{}
		for k, raw := range obj {
			sub, ok := spec.sub[k]
			if !ok {
				return nil, envelope.ED(envelope.CodeForbiddenAction, "unknown params key", k)
			}
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				// 数值/布尔的方言参数：归一为字符串再走同一校验
				var n json.Number
				if err2 := json.Unmarshal(raw, &n); err2 == nil {
					s = n.String()
				} else {
					return bad("invalid params value")
				}
			}
			val, ee := m.validateValue(k, sub, s, section)
			if ee != nil {
				return nil, ee
			}
			res[k] = val
		}
		return res, nil
	case kindJSONArr:
		var arr []string
		if json.Unmarshal([]byte(v), &arr) != nil || len(arr) == 0 || len(arr) > 8 {
			return bad("invalid params array")
		}
		res := make([]any, 0, len(arr))
		for _, el := range arr {
			val, ee := m.validateValue(name, field{kind: kindEnum, allowed: spec.allowed}, el, section)
			if ee != nil {
				return nil, ee
			}
			res = append(res, val)
		}
		return res, nil
	}
	return nil, envelope.E(envelope.CodeInternal, "unhandled kind")
}

// labelLike 短标识：数字/字母/下划线/短横/逗号/点（qmi、SM、B3、41,78,79、93b1…）。
func labelLike(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '_' || c == '-' || c == ',' || c == '.':
		default:
			return false
		}
	}
	return true
}

// execRun 默认执行（无 shell；stdout+stderr 合并）。
func execRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	p, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("executable not found: %s", name)
	}
	c := exec.CommandContext(ctx, p, args...)
	return c.CombinedOutput()
}

// uciModemSections 读 qmodem 的 modem-device section 白名单（生产 provider）。
func uciModemSections() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "uci", "-q", "show", "qmodem").Output()
	if err != nil {
		return nil, err
	}
	var secs []string
	for _, line := range strings.Split(string(out), "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.Trim(strings.TrimSpace(val), `'"`) != "modem-device" {
			continue
		}
		parts := strings.Split(key, ".")
		if len(parts) == 2 && parts[0] == "qmodem" && sectionRe.MatchString(parts[1]) {
			secs = append(secs, parts[1])
		}
	}
	return secs, nil
}

// ubusTTYPorts 读某模组的 AT 端口列表（生产 provider；失败返回空 → 调用方回退字符集校验）。
func ubusTTYPorts(section string) ([]string, error) {
	if !sectionRe.MatchString(section) {
		return nil, fmt.Errorf("invalid section")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ubus", "call", "qmodem", "get_tty_ports",
		fmt.Sprintf(`{"config_section":%q}`, section)).Output()
	if err != nil {
		return nil, err
	}
	var v struct {
		Ports []string `json:"ports"`
	}
	if json.Unmarshal(out, &v) != nil {
		return nil, fmt.Errorf("bad ports payload")
	}
	var ports []string
	for _, p := range v.Ports {
		if portRe.MatchString(p) {
			ports = append(ports, p)
		}
	}
	return ports, nil
}

// hasControlChars 判定控制字符（SMS 内容等文本参数；允许换行/制表）。
func hasControlChars(s string) bool {
	for _, r := range s {
		if r < 0x20 && r != '\n' && r != '\t' {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
