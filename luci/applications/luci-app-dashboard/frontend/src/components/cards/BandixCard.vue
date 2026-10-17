<script setup>
/* Bandix 流量明细卡：在线设备 / WAN 速率合计 / 连接与 DNS 摘要 / Top 设备
 * （契约 §3.7；available!==true 时由注册表整卡隐藏——与 Docker 卡同款谓词）
 * 数据面：WP-4 daemon 慢刷经 bridge 契约 quartz.bandix（WP-8 P2 链路） */
import { computed } from 'vue';
import { Dot, EmptyState } from '../primitives.js';
import { fmtRate, fmtBytes, fmtPct } from '../../lib/format.js';

const props = defineProps({ payload: { type: Object, required: true }, host: { type: Object, default: () => ({}) } });
const t = (s) => (props.host.t ? props.host.t(s) : s);
const b = computed(() => props.payload.bandix);
const top = computed(() => (b.value && b.value.top) || []);
const name = (it) => it.host || it.ip || it.mac;
</script>

<template>
	<template v-if="b">
		<template v-if="b.devices || top.length || (b.wan && (b.wan.rx_bps || b.wan.tx_bps))">
			<div class="qzd-wan">
				<Dot state="ok" />
				<span>{{ t('Online devices') }}</span>
				<span class="qz-mono">{{ b.online || 0 }} / {{ b.devices || 0 }}</span>
				<div class="qzd-summary">
					<div>
						<div class="k">{{ t('Connections') }}</div>
						<div class="v">
							<template v-if="b.conn">{{ b.conn.total }}</template>
							<span v-else class="qzd-muted">{{ t('Not enabled') }}</span>
							<template v-if="b.conn">&middot; {{ t('TCP') }} {{ b.conn.tcp }} &middot; {{ t('UDP') }} {{ b.conn.udp }}</template>
						</div>
					</div>
					<div>
						<div class="k">{{ t('DNS queries') }}</div>
						<div class="v">
							<template v-if="b.dns">{{ b.dns.queries }}</template>
							<span v-else class="qzd-muted">{{ t('Not enabled') }}</span>
						</div>
					</div>
				</div>
			</div>
			<div class="qzd-rate">
				<div>
					<div class="k">{{ t('Download') }}</div>
					<div class="v qzd-down">{{ fmtRate((b.wan && b.wan.rx_bps) || 0) }}</div>
				</div>
				<div>
					<div class="k">{{ t('Upload') }}</div>
					<div class="v">{{ fmtRate((b.wan && b.wan.tx_bps) || 0) }}</div>
				</div>
			</div>
			<table v-if="top.length" class="qzd-table">
				<thead>
					<tr>
						<th class="qzd-w-dev">{{ t('Device') }}</th>
						<th class="qzd-w-b">{{ t('Download') }}</th>
						<th class="qzd-w-b">{{ t('Upload') }}</th>
						<th class="qzd-w-p">{{ t('Share') }}</th>
					</tr>
				</thead>
				<tbody>
					<tr v-for="it in top" :key="it.mac">
						<td>{{ name(it) }}</td>
						<td class="qz-mono">{{ fmtBytes(it.rx_b) }}</td>
						<td class="qz-mono">{{ fmtBytes(it.tx_b) }}</td>
						<td class="qz-mono qzd-dimmer">{{ fmtPct(it.pct) }}</td>
					</tr>
				</tbody>
			</table>
			<div class="qzd-cap">{{ t('Bandix per-device traffic (device-side totals; share of cumulative bytes)') }}</div>
		</template>
		<EmptyState v-else :msg="t('No traffic data yet')" />
	</template>
	<EmptyState v-else :msg="t('Unavailable — collector did not return Bandix data')" />
</template>
