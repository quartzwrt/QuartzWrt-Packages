<script setup>
/* Tailscale 卡（IA v2 R8）：状态 + tailnet 地址 + MagicDNS + 对端在线。
 * 数据面 = 既有 `services.tailscale` 探针（WP-10 包投放；本卡不自行取数）；
 * extras 由 tailscale_probe 从 `tailscale status --json` 提取（零 daemon 改动）。 */
import { computed } from 'vue';
import { Dot, KV, EmptyState } from '../primitives.js';
import { maskIp } from '../../lib/format.js';

const props = defineProps({ payload: { type: Object, required: true }, host: { type: Object, default: () => ({}) } });
const t = (s) => (props.host.t ? props.host.t(s) : s);

const ts = computed(() => (props.payload.services && props.payload.services.tailscale) || null);
const backend = computed(() => (ts.value && ts.value.backend_state) || '');
const stateTone = computed(() => {
	if (!ts.value) return '';
	if (!ts.value.running) return 'err';
	return String(backend.value).toLowerCase() === 'running' ? 'ok' : 'warn';
});
const stateText = computed(() => {
	if (!ts.value) return '';
	if (!ts.value.running) return t('stopped');
	return backend.value || t('running');
});
</script>

<template>
	<template v-if="ts">
		<div class="qzd-wan">
			<Dot :state="stateTone" />
			<span>{{ stateText }}</span>
			<span v-if="ts.version" class="qzd-muted qz-mono">{{ ts.version }}</span>
		</div>
		<KV :k="t('tailnet address')" :v="ts.ip ? maskIp(ts.ip) : '—'" />
		<KV :k="t('MagicDNS')" :v="ts.dns_name || '—'" />
		<KV :k="t('Peers online')" :v="ts.peers_online != null ? String(ts.peers_online) : '—'" />
		<KV :k="t('Backend state')" :v="backend || '—'" />
	</template>
	<EmptyState v-else :msg="t('Not installed')" />
</template>
