<script setup>
/* 蜂窝卡（2026-10-19 设计样板 v2）：
 * 版面分三层 —— 状态头（注册/运营商/制式）、KPI 带（信号/下行/上行/本月用量）、
 * 四栏细节（Radio / Cell & SIM / Connection / Hardware）。
 * 设计口径：首屏回答"信号好不好、跑多快、用了多少"；硬件与小区参数退到第二层，
 * 每栏固定 5–6 行，避免上一版 10 行单列造成的纵向失衡。
 * 数据面 = daemon cellular 慢刷节（契约 §3.9）；敏感信息（IMEI/IMSI/ICCID）不进快照。 */
import { computed } from 'vue';
import { Dot, KV, EmptyState, Sparkline, ErrorChip, IpValue } from '../primitives.js';
import { fmtRate, fmtBytes, fmtTemp } from '../../lib/format.js';
/* 运营商图标（仅取官方 logo 的符号部分；商标归各运营商所有，见 assets/operators/README.md） */
import chinaMobileLogo from '../../assets/operators/china-mobile.svg?raw';
import chinaUnicomLogo from '../../assets/operators/china-unicom.svg?raw';
import chinaTelecomLogo from '../../assets/operators/china-telecom.svg?raw';

const props = defineProps({ payload: { type: Object, required: true }, host: { type: Object, default: () => ({}) } });
const t = (s) => (props.host.t ? props.host.t(s) : s);

/* 运营商元数据：PLMN → 名称 / 官方 logo 符号（SVG，构建期随 app.js 内联）。
 * 商标归各运营商所有，仅用于标识当前驻网运营商；未知 PLMN 不渲染图标。 */
const opMeta = (name, logo) => ({ name, logo });
const PLMN = {
	'46000': opMeta('中国移动', chinaMobileLogo), '46002': opMeta('中国移动', chinaMobileLogo),
	'46004': opMeta('中国移动', chinaMobileLogo), '46007': opMeta('中国移动', chinaMobileLogo),
	'46008': opMeta('中国移动', chinaMobileLogo),
	'46001': opMeta('中国联通', chinaUnicomLogo), '46006': opMeta('中国联通', chinaUnicomLogo),
	'46009': opMeta('中国联通', chinaUnicomLogo),
	'46003': opMeta('中国电信', chinaTelecomLogo), '46005': opMeta('中国电信', chinaTelecomLogo),
	'46011': opMeta('中国电信', chinaTelecomLogo)
};

const c = computed(() => props.payload.cellular);
const modems = computed(() => (c.value && Array.isArray(c.value.modems) ? c.value.modems : []));
const m = computed(() => modems.value[0] || null);
const sig = computed(() => (m.value && m.value.signal) || null);
const ci = computed(() => (m.value && m.value.cell_info) || {});
const cell = computed(() => (m.value && Array.isArray(m.value.serving_cells) && m.value.serving_cells.length
	? m.value.serving_cells[0] : {}));
const dial = computed(() => (m.value && m.value.dial) || null);
const traffic = computed(() => (m.value && m.value.traffic) || null);
const sim = computed(() => (m.value && m.value.sim) || null);
const settings = computed(() => (m.value && m.value.settings) || {});

/* ---- 信号 ---- */
const quality = computed(() => (sig.value && sig.value.quality_pct != null ? Number(sig.value.quality_pct) : null));
const bars = computed(() => (quality.value == null ? 0 : Math.max(0, Math.min(5, Math.round(quality.value / 20)))));
const tone = computed(() => (quality.value == null ? '' : quality.value >= 70 ? 'ok' : quality.value >= 40 ? 'warn' : 'err'));
const qualityText = computed(() => {
	if (quality.value == null) return t('Unavailable');
	if (quality.value >= 70) return t('Good');
	if (quality.value >= 40) return t('Fair');
	return t('Poor');
});
const series = computed(() => (sig.value && sig.value.series && Array.isArray(sig.value.series.rsrp) ? sig.value.series.rsrp : null));
const sigSub = computed(() => {
	const s = sig.value || {};
	const withUnit = (v, unit) => (v == null ? '—' : v + ' ' + unit);
	return 'RSRQ ' + withUnit(s.rsrq, 'dB') + ' · SINR ' + withUnit(s.sinr, 'dB') + ' · RSSI ' + withUnit(s.rssi, 'dBm');
});

/* ---- 运营商 / 制式 ---- */
const operator = computed(() => {
	const v = (m.value && m.value.operator) || (sim.value && sim.value.isp) || '';
	if (!v) return null;
	const meta = PLMN[v];
	return meta
		? { plmn: v, name: meta.name, logo: meta.logo, text: v + ' · ' + meta.name }
		: { plmn: v, name: v, logo: '', text: v };
});
const operatorText = computed(() => (operator.value ? operator.value.text : '—'));

/* ---- KPI：速率 / 用量 ---- */
const dialIpv4 = computed(() => {
	if (dial.value && dial.value.ipv4) return dial.value.ipv4;
	const wan = props.payload.net && props.payload.net.wan;
	if (wan && wan.ifname && String(wan.ifname).startsWith('wwan') && wan.ipv4) return wan.ipv4;
	return '';
});
const rxText = computed(() => (dial.value && dial.value.running ? fmtRate(dial.value.rx_bps || 0) : '—'));
const txText = computed(() => (dial.value && dial.value.running ? fmtRate(dial.value.tx_bps || 0) : '—'));
const usage = computed(() => (traffic.value && traffic.value.available
	? { rx: fmtBytes(traffic.value.rx_bytes), tx: fmtBytes(traffic.value.tx_bytes) }
	: null));
const resetText = computed(() => {
	const r = traffic.value && traffic.value.reset;
	if (!r || !r.enabled) return t('No reset plan');
	const time = ('0' + (r.hour || 0)).slice(-2) + ':00';
	if ((r.type || 'monthly') === 'monthly') return t('Monthly') + ' ' + (r.day || 1) + ' · ' + time;
	if (r.type === 'weekly') return t('Weekly') + ' · ' + time;
	if (r.type === 'daily') return t('Daily') + ' · ' + time;
	return r.type + ' · ' + time;
});

/* ---- 细节栏 ---- */
const cqiText = computed(() => {
	const ul = m.value && m.value.cqi_ul != null ? m.value.cqi_ul : '—';
	const dl = m.value && m.value.cqi_dl != null ? m.value.cqi_dl : '—';
	return ul + ' / ' + dl;
});
const ambrText = computed(() => {
	const ul = m.value && m.value.ambr_ul != null ? m.value.ambr_ul : '—';
	const dl = m.value && m.value.ambr_dl != null ? m.value.ambr_dl : '—';
	return ul + ' / ' + dl + ' Mbps';
});
const voltageText = computed(() => (m.value && m.value.voltage_mv ? m.value.voltage_mv + ' mV' : '—'));
const vendorText = computed(() => {
	const v = (m.value && m.value.vendor) || '';
	return v ? v.charAt(0).toUpperCase() + v.slice(1) : '—';
});
const cellPick = (a, b) => ci.value[a] || cell.value[b] || '—';
const mccMnc = computed(() => (ci.value.mcc ? ci.value.mcc + ' / ' + (ci.value.mnc || '—') : '—'));
const swText = computed(() => (sim.value ? (sim.value.switchable ? t('Yes') : t('No')) : '—'));
const bandText = computed(() => {
	if (cell.value.band) return cell.value.band;
	const b = ci.value.band;
	if (!b) return '—';
	return /NR/i.test(m.value && m.value.network_mode ? m.value.network_mode : '') ? 'n' + b : 'B' + b;
});
const bandShort = computed(() => (bandText.value === '—' ? '—' : bandText.value.replace(/^NR\s+/i, '')));
const pciText = computed(() => cellPick('pci', 'pci'));
const arfcnText = computed(() => cellPick('arfcn', 'channel'));
const bwText = computed(() => cellPick('ul_bandwidth', 'ul_bandwidth') + ' / ' + cellPick('dl_bandwidth', 'dl_bandwidth') + ' MHz');
const scsText = computed(() => {
	const v = cellPick('scs', 'scs');
	return v === '—' ? v : v + ' kHz';
});
</script>

<template>
	<template v-if="c && m">
		<!-- 状态头：注册态 / 运营商 / 制式；右侧只留模组名 -->
		<div class="qzd-c2-head">
			<span class="qzd-c2-status">
				<Dot :state="m.registered ? 'ok' : 'err'" />
				{{ m.registered ? t('Registered') : t('Not registered') }}
			</span>
			<span class="qzd-c2-op">
				<span v-if="operator && operator.logo" class="qzd-c2-oplogo" :title="operator.name"
					v-html="operator.logo"></span>
				{{ operatorText }}
			</span>
			<span v-if="m.network_mode" class="qzd-c2-chip">{{ m.network_mode }}</span>
			<span v-if="m.network_type" class="qzd-c2-chip qzd-muted">{{ m.network_type }}</span>
			<span class="qzd-c2-meta">{{ m.model }}</span>
		</div>

		<!-- KPI 带：信号（含放大的 RSRP 曲线）/ 上下行 / 本月用量 / 服务小区 -->
		<div class="qzd-c2-kpis">
			<div class="qzd-c2-kpi qzd-c2-kpi-signal">
				<div class="qzd-c2-k">
					<span>{{ t('Signal') }}</span>
					<span class="qzd-meter" :class="tone">
						<i v-for="n in 5" :key="n" :class="{ on: n <= bars }"></i>
					</span>
				</div>
				<div class="qzd-c2-signalbody">
					<div class="qzd-c2-v">
						<span class="qzd-c2-big qz-mono">{{ sig && sig.rsrp != null ? sig.rsrp : '—' }}</span>
						<small>dBm</small>
						<span class="qzd-c2-qual" :class="tone">{{ qualityText }}</span>
					</div>
					<Sparkline v-if="series" :values="series" fill axis :height="46" class="qzd-c2-chart" />
				</div>
				<div class="qzd-c2-sub qz-mono">{{ sigSub }}</div>
			</div>
			<div class="qzd-c2-kpi qzd-c2-kpi-rate">
				<div class="qzd-c2-k">{{ t('Rate') }}</div>
				<div class="qzd-c2-ratepair">
					<div>
						<div class="k">{{ t('Downlink') }}</div>
						<div class="v qzd-down">{{ rxText }}</div>
					</div>
					<div>
						<div class="k">{{ t('Uplink') }}</div>
						<div class="v">{{ txText }}</div>
					</div>
				</div>
				<div class="qzd-c2-sub">{{ dial && dial.running ? t('WAN traffic') : t('Not connected') }}</div>
			</div>
			<div class="qzd-c2-kpi">
				<div class="qzd-c2-k">{{ t('This month') }}</div>
				<template v-if="usage">
					<div class="qzd-c2-v"><span>{{ usage.rx }}</span><small>&darr;</small></div>
					<div class="qzd-c2-sub">{{ usage.tx }} &uarr; &middot; {{ resetText }}</div>
				</template>
				<div v-else class="qzd-c2-v qzd-muted">{{ t('Not supported') }}</div>
			</div>
			<div class="qzd-c2-kpi qzd-c2-kpi-cell">
				<div class="qzd-c2-k">{{ t('Serving cell') }}</div>
				<div class="qzd-c2-v"><span class="qzd-c2-cellband">{{ bandShort }}</span></div>
				<div class="qzd-c2-sub qz-mono">PCI {{ pciText }} &middot; ARFCN {{ arfcnText }}</div>
			</div>
		</div>

		<!-- 细节四栏：每栏 5–6 行，信息层级低于 KPI 带 -->
		<div class="qzd-cols4 qzd-c2-cols">
			<div>
				<div class="qzd-sublabel">{{ t('Radio') }}</div>
				<KV :k="t('Band')" :v="bandText" />
				<KV :k="t('ARFCN')" :v="cellPick('arfcn', 'channel')" />
				<KV :k="t('PCI')" :v="cellPick('pci', 'pci')" />
				<KV :k="t('UL / DL BW')" :v="bwText" />
				<KV :k="t('SCS')" :v="scsText" />
				<KV :k="t('Duplex')" :v="cellPick('duplex', 'duplex')" />
			</div>
			<div>
				<div class="qzd-sublabel">{{ t('Cell & SIM') }}</div>
				<KV :k="t('MCC / MNC')" :v="mccMnc" />
				<KV :k="t('TAC')" :v="cellPick('tac', 'tac')" />
				<KV :k="t('Cell ID')" :v="cellPick('cell_id', 'cell_id')" />
				<KV :k="t('SIM Status')" :v="sim && sim.status ? sim.status : '—'" />
				<KV :k="t('SIM Slot')" :v="sim && sim.slot ? sim.slot : '—'" />
				<KV :k="t('Switchable')" :v="swText" />
			</div>
			<div>
				<div class="qzd-sublabel">{{ t('Connection') }}</div>
				<KV :k="t('IPv4')">
					<IpValue :value="dialIpv4" :show-label="t('Click to reveal')" :hide-label="t('Click to hide')" />
				</KV>
				<KV :k="t('CQI UL / DL')" :v="cqiText" />
				<KV :k="t('AMBR UL / DL')" :v="ambrText" />
				<KV :k="t('USB mode')" :v="settings.usb_mode || '—'" />
				<KV :k="t('Preference')" :v="settings.prefer || '—'" />
			</div>
			<div>
				<div class="qzd-sublabel">{{ t('Hardware') }}</div>
				<KV :k="t('Manufacturer')" :v="vendorText" />
				<KV :k="t('Revision')" :v="m.firmware || '—'" />
				<KV :k="t('AT Port')" :v="m.at_port || '—'" />
				<KV :k="t('Voltage')" :v="voltageText" />
				<KV :k="t('Temperature')" :v="fmtTemp(m.temp_c)" />
				<KV :k="t('Connect Status')" :v="m.connect_status || (dial && dial.running ? t('Connected') : '—')" />
			</div>
		</div>

		<ErrorChip v-if="c.errors && c.errors.length" :msg="c.errors.join('; ')" />
		<div class="qzd-c2-foot">
			<span>{{ t('IMEI / IMSI / ICCID stay on the QModem page') }}</span>
			<span class="qzd-muted">{{ t('QModem slow lane · 10s') }}</span>
		</div>
	</template>
	<EmptyState v-else :msg="t('No cellular modem')" />
</template>
