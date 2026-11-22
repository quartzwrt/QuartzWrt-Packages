<script setup>
/* 电池卡（契约 §3.10；Photonicat 一代 pcat-mgr/FUSE）。
 * 数据面 = daemon battery 节；卡片不自行取数。
 * 版式（2026-11-22 重做）：左侧电池图形 + 大号电量，右侧三项指标——
 * 与 System(4) 同行时撑满 8 列卡高，避免三行 KV 留下大片空白。 */
import { computed } from 'vue';
import { Dot, EmptyState } from '../primitives.js';

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

const charging = computed(() => String((b.value && b.value.status) || '') === 'Charging');

const capacity = computed(() => {
	const v = Number(b.value && b.value.capacity);
	if (!Number.isFinite(v)) return 0;
	return Math.min(100, Math.max(0, v));
});

const levelClass = computed(() => {
	if (capacity.value <= 20) return 'is-err';
	return charging.value ? 'is-ok' : 'is-accent';
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

const technologyText = computed(() => (b.value && b.value.technology) || '—');
const manufacturerText = computed(() => (b.value && b.value.manufacturer) || '');
const chargeLabel = computed(() => t('Charge') + ' ' + capacity.value + '%');
</script>

<template>
	<template v-if="b && b.available">
		<div class="qzd-batt-head">
			<Dot :state="dotState" />
			<span class="qzd-batt-status">{{ statusText }}</span>
			<span class="qzd-muted">{{ b.charger_online ? t('Plugged in') : t('Not plugged in') }}</span>
			<span v-if="manufacturerText" class="qzd-muted qzd-batt-meta">{{ manufacturerText }}</span>
		</div>

		<div class="qzd-batt-body">
			<div class="qzd-batt-gauge">
				<div class="qzd-batt-glyph" role="progressbar" :aria-label="chargeLabel"
					aria-valuemin="0" aria-valuemax="100" :aria-valuenow="capacity">
					<div class="qzd-batt-shell">
						<div class="qzd-batt-level" :class="levelClass" :style="{ width: capacity + '%' }"></div>
					</div>
					<div class="qzd-batt-tip"></div>
				</div>
				<div class="qzd-batt-readout">
					<span class="qzd-batt-pct">{{ capacity }}<span>%</span></span>
					<span class="qzd-batt-label">{{ t('Charge') }}</span>
				</div>
			</div>

			<div class="qzd-batt-stats">
				<div class="qzd-batt-stat">
					<span class="k">{{ t('Voltage') }}</span>
					<span class="v">{{ voltageText }}</span>
				</div>
				<div class="qzd-batt-stat">
					<span class="k">{{ t('Energy') }}</span>
					<span class="v">{{ energyText }}</span>
				</div>
				<div class="qzd-batt-stat">
					<span class="k">{{ t('Chemistry') }}</span>
					<span class="v">{{ technologyText }}</span>
				</div>
			</div>
		</div>

		<div v-if="b.present === false" class="qzd-errchip">
			<Dot state="warn" /><span>{{ t('Battery not present') }}</span>
		</div>
	</template>
	<EmptyState v-else :msg="t('Unavailable')" />
</template>
