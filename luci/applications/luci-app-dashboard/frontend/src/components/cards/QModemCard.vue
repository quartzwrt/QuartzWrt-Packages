<script setup>
/* 蜂窝卡（IA v2 R19 / DESIGN §5.1）：四栏——信号 / 服务小区 / 拨号 / 流量与 SIM。
 * 数据面 = daemon `cellular` 慢刷节（契约 §3.9，10s，与 App/网页同源）；卡片不自行取数。
 * 四栏字段只取快照已有字段（APN/DNS/MTU/短信存储属页面级读，v1 不上卡）。 */
import { computed } from 'vue';
import { Dot, KV, EmptyState, Sparkline, ErrorChip } from '../primitives.js';
import { fmtRate, fmtBytes, fmtTemp, maskIp } from '../../lib/format.js';

const props = defineProps({ payload: { type: Object, required: true }, host: { type: Object, default: () => ({}) } });
const t = (s) => (props.host.t ? props.host.t(s) : s);

const c = computed(() => props.payload.cellular);
const modems = computed(() => (c.value && Array.isArray(c.value.modems) ? c.value.modems : []));
const m = computed(() => modems.value[0] || null);
const sig = computed(() => (m.value && m.value.signal) || null);
const cells = computed(() => (m.value && Array.isArray(m.value.serving_cells) ? m.value.serving_cells : []));
const pcc = computed(() => cells.value.find((x) => /LTE/i.test(x.rat || '') || x.role === 'PCC') || cells.value[0] || null);
const nr = computed(() => cells.value.find((x) => /NR|5G/i.test(x.rat || '')) || (cells.value.length > 1 ? cells.value[1] : null));
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

/* 上游 band 可能已含 RAT（如 "NR n78"），不再重复 rat；role 如 pcc 统一大写。 */
const cellLabel = (cell) => {
	const role = cell.role ? String(cell.role).toUpperCase() : '';
	const band = cell.band || cell.rat || '';
	return [role, band].filter(Boolean).join(' · ');
};
const cellValue = (cell) => [cell.channel, cell.pci ? 'PCI ' + cell.pci : ''].filter(Boolean).join(' · ');
/* 蜂窝拨号 IPv4：上游 dial.ipv4 缺失时，退回当前 WAN 的 IPv4（仅当 WAN 是 wwan）。 */
const dialIpv4 = computed(() => {
	if (dial.value && dial.value.ipv4) return dial.value.ipv4;
	const wan = props.payload.net && props.payload.net.wan;
	if (wan && wan.ifname && String(wan.ifname).startsWith('wwan') && wan.ipv4) return wan.ipv4;
	return '';
});
const cellBandwidth = (cell) => {
	if (!cell) return '—';
	const parts = [];
	if (cell.dl_bandwidth) parts.push(cell.dl_bandwidth + ' MHz');
	if (cell.scs) parts.push(cell.scs + ' kHz');
	return parts.length ? parts.join(' · ') : '—';
};

const resetText = computed(() => {
	const r = traffic.value && traffic.value.reset;
	if (!r || !r.enabled) return '—';
	const type = r.type || 'monthly';
	const time = ('0' + (r.hour || 0)).slice(-2) + ':00';
	if (type === 'monthly') return t('Monthly') + ' ' + (r.day || 1) + ' · ' + time;
	if (type === 'weekly') return t('Weekly') + ' · ' + time;
	if (type === 'daily') return t('Daily') + ' · ' + time;
	return type + ' · ' + time;
});

const simText = computed(() => {
	if (!sim.value) return '—';
	return [sim.value.status, sim.value.slot ? t('slot') + ' ' + sim.value.slot : ''].filter(Boolean).join(' · ');
});
const rateText = computed(() => (dial.value
	? '↓ ' + fmtRate(dial.value.rx_bps || 0) + ' · ↑ ' + fmtRate(dial.value.tx_bps || 0)
	: '—'));
const usageText = computed(() => (traffic.value && traffic.value.available
	? '↓ ' + fmtBytes(traffic.value.rx_bytes) + ' · ↑ ' + fmtBytes(traffic.value.tx_bytes)
	: t('Not supported')));
</script>

<template>
	<template v-if="c && m">
		<div class="qzd-cellular-head">
			<Dot :state="m.registered ? 'ok' : 'err'" />
			<span>{{ m.registered ? t('Registered') : t('Not registered') }}</span>
			<span class="qzd-muted">{{ m.network_mode }}<template v-if="m.operator"> · {{ m.operator }}</template></span>
			<span class="qzd-cellular-meta qzd-muted">
				{{ m.model }}<template v-if="m.firmware"> · {{ m.firmware }}</template><template v-if="m.temp_c != null"> · {{ fmtTemp(m.temp_c) }}</template>
			</span>
		</div>

		<div class="qzd-cols4">
			<div>
				<div class="qzd-kv">
					<span class="qzd-kv-k">{{ t('Signal quality') }}</span>
					<span class="qzd-meter" :class="tone">
						<i v-for="n in 5" :key="n" :class="{ on: n <= bars }"></i>
					</span>
				</div>
				<div class="qzd-cap qzd-cellular-quality">
					<template v-if="sig && sig.rsrp != null">RSRP {{ sig.rsrp }} dBm · </template>{{ qualityText }}
				</div>
				<div v-if="sig" class="qzd-mstrip">
					<div class="qzd-mcell"><div class="k">RSRP</div><div class="v qz-mono">{{ sig.rsrp != null ? sig.rsrp : '—' }}<small>dBm</small></div></div>
					<div class="qzd-mcell"><div class="k">RSRQ</div><div class="v qz-mono">{{ sig.rsrq != null ? sig.rsrq : '—' }}<small>dB</small></div></div>
					<div class="qzd-mcell"><div class="k">SINR</div><div class="v qz-mono">{{ sig.sinr != null ? sig.sinr : '—' }}<small>dB</small></div></div>
					<div class="qzd-mcell"><div class="k">RSSI</div><div class="v qz-mono">{{ sig.rssi != null ? sig.rssi : '—' }}<small>dBm</small></div></div>
				</div>
				<Sparkline v-if="series" :values="series" fill class="qzd-spark-mini" />
				<div v-if="series" class="qzd-legendline">
					<span>{{ t('RSRP history') }}</span><span>{{ t('last 10 min') }}</span>
				</div>
			</div>

			<div>
				<KV :k="t('Registration')" :v="m.registered ? (m.network_mode + (m.operator ? ' · ' + m.operator : '')) : t('Not registered')" />
				<template v-if="pcc">
					<KV :k="cellLabel(pcc)" :v="cellValue(pcc)" />
					<KV :k="t('Bandwidth · SCS')" :v="cellBandwidth(pcc)" />
				</template>
				<template v-if="nr && nr !== pcc">
					<KV :k="cellLabel(nr)" :v="cellValue(nr)" />
					<KV :k="t('Bandwidth · SCS')" :v="cellBandwidth(nr)" />
				</template>
				<KV :k="t('Preference')" :v="settings.prefer || '—'" />
			</div>

			<div>
				<div class="qzd-kv">
					<span class="qzd-kv-k">{{ t('Dial') }}</span>
					<span class="qzd-kv-v">
						<Dot :state="dial && dial.running ? 'ok' : 'err'" />
						{{ dial && dial.running ? t('Connected') : t('Disconnected') }}
					</span>
				</div>
				<KV :k="t('IPv4')" :v="dialIpv4 ? maskIp(dialIpv4) : '—'" />
				<KV :k="t('Rate')" :v="rateText" />
				<KV :k="t('USB mode')" :v="settings.usb_mode || '—'" />
			</div>

			<div>
				<KV :k="t('This month')" :v="usageText" />
				<KV :k="t('Reset plan')" :v="resetText" />
				<KV :k="t('SIM')" :v="simText" />
				<KV :k="t('Temperature')" :v="fmtTemp(m.temp_c)" />
			</div>
		</div>

		<ErrorChip v-if="c.errors && c.errors.length" :msg="c.errors.join('; ')" />
		<div class="qzd-cap">{{ t('Cellular data from QModem (10s slow lane; page-level reads stay on the QModem page)') }}</div>
	</template>
	<EmptyState v-else :msg="t('No cellular modem')" />
</template>
