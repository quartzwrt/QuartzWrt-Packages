<script setup>
/* 系统资源卡：负载 / CPU / 内存 / 交换 / 存储 / 温度（契约 §3.2） */
import { computed } from 'vue';
import { KV, EmptyState } from '../primitives.js';
import { fmtBytes, fmtPct, fmtTemp, fmtMHz } from '../../lib/format.js';

const props = defineProps({ payload: { type: Object, required: true }, host: { type: Object, default: () => ({}) } });
const t = (s) => (props.host.t ? props.host.t(s) : s);
const sys = computed(() => props.payload.system);
const cpuSummary = computed(() => {
	const s = sys.value;
	if (!s) return '';
	const parts = [];
	if (s.cores) parts.push(s.cores + 'C');
	if (s.cpu_mhz) parts.push(fmtMHz(s.cpu_mhz) + (s.cpu_max_mhz ? ' / ' + fmtMHz(s.cpu_max_mhz) : ''));
	return parts.join(' · ');
});
</script>

<template>
	<EmptyState v-if="!sys" :msg="t('Unavailable')" />
	<template v-else>
		<div v-if="sys.cpu_model || cpuSummary" class="qzd-syscpu" :title="sys.cpu_model || null">
			{{ sys.cpu_model || t('CPU') }}<template v-if="cpuSummary"> <span class="qzd-muted">· {{ cpuSummary }}</span></template>
		</div>
		<KV :k="t('Load (1/5/15)')" :v="sys.load.map((x) => x.toFixed(2)).join(' / ')" />
		<KV :k="t('CPU usage')" :bar="sys.cpu_pct" :v="fmtPct(sys.cpu_pct)" />
		<KV :k="t('Processes')" :muted="!sys.procs" :v="sys.procs ? String(sys.procs) : '—'" />
		<KV :k="t('Memory')" :bar="sys.mem.pct" :v="fmtBytes(sys.mem.used) + ' / ' + fmtBytes(sys.mem.total)" />
		<KV :k="t('Swap')" :muted="!sys.swap || !sys.swap.total"
			:v="sys.swap && sys.swap.total ? fmtBytes(sys.swap.used) + ' / ' + fmtBytes(sys.swap.total) : t('not enabled')" />
		<KV :k="t('Storage (/)')" :bar="sys.rootfs.pct" :v="fmtBytes(sys.rootfs.used) + ' / ' + fmtBytes(sys.rootfs.total)" />
		<KV :k="t('Temperature')" :muted="sys.temp_c == null" :v="sys.temp_c == null ? t('no sensor') : fmtTemp(sys.temp_c)" />
	</template>
</template>
