<script setup>
/* 蜂窝卡（2026-10-19 真机反馈轮，按 qmodem 首页信息项重排）：
 * 顶部一条信号带（质量条 + 四指标 + RSRP 曲线），下方四栏
 * Base / SIM / Network / Cell；数据面 = daemon cellular 慢刷节（契约 §3.9）。
 * 敏感信息（IMEI/IMSI/ICCID）不加快照，只在 QModem 页面显示。 */
import { computed } from 'vue';
import { Dot, KV, EmptyState, Sparkline, ErrorChip } from '../primitives.js';
import { fmtRate, fmtBytes, fmtTemp, maskIp } from '../../lib/format.js';

const props = defineProps({ payload: { type: Object, required: true }, host: { type: Object, default: () => ({}) } });
const t = (s) => (props.host.t ? props.host.t(s) : s);

const PLMN = {
	'46000': '中国移动', '46002': '中国移动', '46004': '中国移动', '46007': '中国移动', '46008': '中国移动',
	'46001': '中国联通', '46006': '中国联通', '46009': '中国联通',
	'46003': '中国电信', '46005': '中国电信', '46011': '中国电信'
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

const operatorText = computed(() => {
	const v = (m.value && m.value.operator) || (sim.value && sim.value.isp) || '';
	if (!v) return '—';
	return PLMN[v] ? v + ' · ' + PLMN[v] : v;
});
const dialIpv4 = computed(() => {
	if (dial.value && dial.value.ipv4) return dial.value.ipv4;
	const wan = props.payload.net && props.payload.net.wan;
	if (wan && wan.ifname && String(wan.ifname).startsWith('wwan') && wan.ipv4) return wan.ipv4;
	return '';
});
const rateText = computed(() => (dial.value
	? '↓ ' + fmtRate(dial.value.rx_bps || 0) + ' · ↑ ' + fmtRate(dial.value.tx_bps || 0)
	: '—'));
const usageText = computed(() => (traffic.value && traffic.value.available
	? '↓ ' + fmtBytes(traffic.value.rx_bytes) + ' · ↑ ' + fmtBytes(traffic.value.tx_bytes)
	: t('Not supported')));
const resetText = computed(() => {
	const r = traffic.value && traffic.value.reset;
	if (!r || !r.enabled) return '—';
	const time = ('0' + (r.hour || 0)).slice(-2) + ':00';
	if ((r.type || 'monthly') === 'monthly') return t('Monthly') + ' ' + (r.day || 1) + ' · ' + time;
	if (r.type === 'weekly') return t('Weekly') + ' · ' + time;
	if (r.type === 'daily') return t('Daily') + ' · ' + time;
	return r.type + ' · ' + time;
});
const cqiText = computed(() => {
	const ul = m.value && m.value.cqi_ul != null ? m.value.cqi_ul : '—';
	const dl = m.value && m.value.cqi_dl != null ? m.value.cqi_dl : '—';
	return ul + ' / ' + dl;
});
const ambrText = computed(() => {
	const ul = m.value && m.value.ambr_ul != null ? m.value.ambr_ul : '—';
	const dl = m.value && m.value.ambr_dl != null ? m.value.ambr_dl : '—';
	return ul + ' / ' + dl;
});
const voltageText = computed(() => (m.value && m.value.voltage_mv ? m.value.voltage_mv + ' mV' : '—'));
const vendorText = computed(() => {
	const v = (m.value && m.value.vendor) || '';
	return v ? v.charAt(0).toUpperCase() + v.slice(1) : '—';
});
const cellPick = (a, b) => ci.value[a] || cell.value[b] || '—';
const cellMode = computed(() => ci.value.network_mode || (m.value && m.value.network_mode) || '—');
const mccMnc = computed(() => (ci.value.mcc ? ci.value.mcc + ' / ' + (ci.value.mnc || '—') : '—'));
const swText = computed(() => (sim.value ? (sim.value.switchable ? t('Yes') : t('No')) : '—'));
</script>

<template>
	<template v-if="c && m">
		<div class="qzd-cellular-head">
			<Dot :state="m.registered ? 'ok' : 'err'" />
			<span>{{ m.registered ? t('Registered') : t('Not registered') }}</span>
			<span class="qzd-muted">{{ m.network_mode }}<template v-if="operatorText !== '—'"> · {{ operatorText }}</template></span>
			<span class="qzd-cellular-meta qzd-muted">
				{{ m.model }}<template v-if="m.firmware"> · {{ m.firmware }}</template><template v-if="m.temp_c != null"> · {{ fmtTemp(m.temp_c) }}</template>
			</span>
		</div>

		<!-- 信号带：质量条 + 四指标 + RSRP 曲线（不再单独占一栏，减少留白） -->
		<div class="qzd-sigstrip">
			<div class="qzd-sig-quality">
				<div class="qzd-sig-title">{{ t('Signal quality') }}</div>
				<span class="qzd-meter" :class="tone">
					<i v-for="n in 5" :key="n" :class="{ on: n <= bars }"></i>
				</span>
				<div class="qzd-cap qzd-sig-text">
					<template v-if="sig && sig.rsrp != null">RSRP {{ sig.rsrp }} dBm · </template>{{ qualityText }}
				</div>
			</div>
			<div v-if="sig" class="qzd-mstrip qzd-mstrip-4">
				<div class="qzd-mcell"><div class="k">RSRP</div><div class="v qz-mono">{{ sig.rsrp != null ? sig.rsrp : '—' }}<small>dBm</small></div></div>
				<div class="qzd-mcell"><div class="k">RSRQ</div><div class="v qz-mono">{{ sig.rsrq != null ? sig.rsrq : '—' }}<small>dB</small></div></div>
				<div class="qzd-mcell"><div class="k">SINR</div><div class="v qz-mono">{{ sig.sinr != null ? sig.sinr : '—' }}<small>dB</small></div></div>
				<div class="qzd-mcell"><div class="k">RSSI</div><div class="v qz-mono">{{ sig.rssi != null ? sig.rssi : '—' }}<small>dBm</small></div></div>
			</div>
			<Sparkline v-if="series" :values="series" fill class="qzd-spark qzd-sig-spark" />
		</div>

		<div class="qzd-cols4 qzd-cols4-tight">
			<!-- Base Information -->
			<div>
				<div class="qzd-sublabel">{{ t('Base') }}</div>
				<KV :k="t('Name')" :v="m.model || '—'" />
				<KV :k="t('Manufacturer')" :v="vendorText" />
				<KV :k="t('Revision')" :v="m.firmware || '—'" />
				<KV :k="t('AT Port')" :v="m.at_port || '—'" />
				<KV :k="t('Temperature')" :v="fmtTemp(m.temp_c)" />
				<KV :k="t('Voltage')" :v="voltageText" />
				<KV :k="t('Connect Status')" :v="m.connect_status || (dial && dial.running ? t('Connected') : '—')" />
			</div>

			<!-- SIM Information -->
			<div>
				<div class="qzd-sublabel">{{ t('SIM') }}</div>
				<KV :k="t('SIM Status')" :v="sim && sim.status ? sim.status : '—'" />
				<KV :k="t('Operator')" :v="operatorText" />
				<KV :k="t('SIM Slot')" :v="sim && sim.slot ? sim.slot : '—'" />
				<KV :k="t('Switchable')" :v="swText" />
				<KV :k="t('This month')" :v="usageText" />
				<KV :k="t('Reset plan')" :v="resetText" />
				<div class="qzd-cap qzd-sim-note">{{ t('IMEI / IMSI / ICCID stay on the QModem page') }}</div>
			</div>

			<!-- Network Information -->
			<div>
				<div class="qzd-sublabel">{{ t('Network') }}</div>
				<KV :k="t('Network Type')" :v="m.network_type || m.network_mode || '—'" />
				<KV :k="t('CQI UL / DL')" :v="cqiText" />
				<KV :k="t('AMBR UL / DL')" :v="ambrText" />
				<KV :k="t('Rate')" :v="rateText" />
				<KV :k="t('IPv4')" :v="dialIpv4 ? maskIp(dialIpv4) : '—'" />
				<KV :k="t('USB mode')" :v="settings.usb_mode || '—'" />
				<KV :k="t('Preference')" :v="settings.prefer || '—'" />
			</div>

			<!-- Cell Information -->
			<div>
				<div class="qzd-sublabel">{{ t('Cell') }}</div>
				<KV :k="t('Network Mode')" :v="cellMode" />
				<KV :k="t('Duplex')" :v="cellPick('duplex', 'duplex')" />
				<KV :k="t('MCC / MNC')" :v="mccMnc" />
				<KV :k="t('TAC')" :v="cellPick('tac', 'tac')" />
				<KV :k="t('Cell ID')" :v="cellPick('cell_id', 'cell_id')" />
				<KV :k="t('PCI')" :v="cellPick('pci', 'pci')" />
				<KV :k="t('ARFCN')" :v="cellPick('arfcn', 'channel')" />
				<KV :k="t('Band')" :v="cellPick('band', 'band')" />
				<KV :k="t('UL / DL BW')" :v="cellPick('ul_bandwidth', 'ul_bandwidth') + ' / ' + cellPick('dl_bandwidth', 'dl_bandwidth')" />
				<KV :k="t('SCS')" :v="cellPick('scs', 'scs')" />
			</div>
		</div>

		<ErrorChip v-if="c.errors && c.errors.length" :msg="c.errors.join('; ')" />
		<div class="qzd-cap">{{ t('Cellular data from QModem (10s slow lane; page-level reads stay on the QModem page)') }}</div>
	</template>
	<EmptyState v-else :msg="t('No cellular modem')" />
</template>
