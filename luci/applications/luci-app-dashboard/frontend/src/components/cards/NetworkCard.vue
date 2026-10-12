<script setup>
/* 网络吞吐卡：WAN 状态 + 实时上下行 + 60 点 sparkline（契约 §3.3） */
import { computed } from 'vue';
import { Dot, Sparkline } from '../primitives.js';
import { fmtRate } from '../../lib/format.js';

const props = defineProps({ payload: { type: Object, required: true }, host: { type: Object, default: () => ({}) } });
const t = (s) => (props.host.t ? props.host.t(s) : s);
const net = computed(() => props.payload.net);
</script>

<template>
	<template v-if="net && net.wan">
		<div class="qzd-wan">
			<Dot :state="net.wan.up ? 'ok' : 'err'" />
			<span>{{ t(net.wan.up ? 'online' : 'offline') }}</span>
			<span class="qzd-muted">{{ net.wan.ifname }} &middot; {{ net.wan.proto }}</span>
			<span class="qz-mono">{{ net.wan.ipv4 }}</span>
		</div>
		<div class="qzd-rate">
			<div>
				<div class="k">{{ t('Download') }}</div>
				<div class="v qzd-down">{{ fmtRate(net.wan.rx_bps) }}</div>
			</div>
			<div>
				<div class="k">{{ t('Upload') }}</div>
				<div class="v">{{ fmtRate(net.wan.tx_bps) }}</div>
			</div>
		</div>
		<Sparkline :series="net.series || null" :rx="net.wan.rx_bps || 0" :tx="net.wan.tx_bps || 0" />
		<div class="qzd-cap">{{ t('Download (blue) / Upload (grey) — last 2 minutes, daemon ring buffer') }}</div>
	</template>
	<div v-else class="qzd-empty">{{ t('Unavailable') }}</div>
</template>
