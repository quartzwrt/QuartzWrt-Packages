<script setup>
/* 网络吞吐卡：WAN 状态 + 实时上下行 + 60 点 sparkline（契约 §3.3） */
import { computed } from 'vue';
import { Dot, KV, IpValue, Sparkline } from '../primitives.js';
import { fmtRate } from '../../lib/format.js';

const props = defineProps({ payload: { type: Object, required: true }, host: { type: Object, default: () => ({}) } });
const t = (s) => (props.host.t ? props.host.t(s) : s);
const net = computed(() => props.payload.net);
/* y 轴标签：0 显示为 0，其余沿用卡片内的速率格式（Kbps/Mbps） */
const axisRate = (bps) => (bps ? fmtRate(bps) : '0');
</script>

<template>
	<template v-if="net && net.wan">
		<div class="qzd-wan">
			<Dot :state="net.wan.up ? 'ok' : 'err'" />
			<span>{{ t(net.wan.up ? 'online' : 'offline') }}</span>
			<span class="qzd-muted">{{ net.wan.ifname }} &middot; {{ net.wan.proto }}</span>
		</div>
		<KV :k="t('IPv4')">
			<IpValue :value="net.wan.ipv4" :show-label="t('Click to reveal')" :hide-label="t('Click to hide')" />
		</KV>
		<KV v-if="net.wan.ipv6" :k="t('IPv6')">
			<IpValue :value="net.wan.ipv6" :show-label="t('Click to reveal')" :hide-label="t('Click to hide')" />
		</KV>
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
		<Sparkline :series="net.series || null" :rx="net.wan.rx_bps || 0" :tx="net.wan.tx_bps || 0"
			axis :axis-format="axisRate" />
		<div class="qzd-cap">{{ t('Download (blue) / Upload (grey) — last 2 minutes, daemon ring buffer') }}</div>
	</template>
	<div v-else class="qzd-empty">{{ t('Unavailable') }}</div>
</template>
