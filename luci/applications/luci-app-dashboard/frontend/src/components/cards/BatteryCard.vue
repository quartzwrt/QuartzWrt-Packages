<script setup>
/* 电池卡（契约 §3.10；Photonicat 一代 pcat-mgr/FUSE）。
 * 数据面 = daemon battery 节；卡片不自行取数。 */
import { computed } from 'vue';
import { KV, Dot, EmptyState } from '../primitives.js';

const props = defineProps({ payload: { type: Object, required: true }, host: { type: Object, default: () => ({}) } });
const t = (s) => (props.host.t ? props.host.t(s) : s);
const b = computed(() => props.payload.battery);

const statusText = computed(() => {
	const s = String((b.value && b.value.status) || 'Unknown');
	return {
		Charging: t('Charging'),
		Discharging: t('Discharging'),
		Full: t('Full')
	}[s] || t('Unknown');
});

const dotState = computed(() => {
	const v = b.value || {};
	return v.charger_online === true || v.status === 'Full' ? 'ok' : 'warn';
});

const capacity = computed(() => {
	const v = Number(b.value && b.value.capacity);
	if (!Number.isFinite(v)) return 0;
	return Math.min(100, Math.max(0, v));
});

const voltageText = computed(() => {
	const mv = Number(b.value && b.value.voltage_mv);
	return Number.isFinite(mv) && mv > 0 ? (mv / 1000).toFixed(2) + ' V' : '—';
});

const energyText = computed(() => {
	const now = Number(b.value && b.value.energy_now_wh);
	const full = Number(b.value && b.value.energy_full_wh);
	if (!Number.isFinite(full) || full <= 0) return '—';
	return (Number.isFinite(now) ? now : 0).toFixed(1) + ' / ' + full.toFixed(1) + ' Wh';
});
</script>

<template>
	<template v-if="b && b.available">
		<div class="qzd-batt-head">
			<Dot :state="dotState" />
			<span>{{ statusText }}</span>
			<span class="qzd-muted">{{ b.charger_online ? t('Plugged in') : t('Not plugged in') }}</span>
			<span v-if="b.technology || b.manufacturer" class="qzd-muted qzd-batt-meta">
				{{ [b.technology, b.manufacturer].filter(Boolean).join(' · ') }}
			</span>
		</div>

		<KV :k="t('Charge')" :bar="capacity" :v="capacity + '%'" />
		<KV :k="t('Voltage')" :v="voltageText" />
		<KV :k="t('Energy')" :v="energyText" />

		<div v-if="b.present === false" class="qzd-errchip">
			<Dot state="warn" /><span>{{ t('Battery not present') }}</span>
		</div>
	</template>
	<EmptyState v-else :msg="t('Unavailable')" />
</template>
