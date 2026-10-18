<script setup>
/* Bandix 流量明细卡：总体限速策略 + 在线设备/连接/DNS 摘要 + 设备表（速率/限速策略）
 * （契约 §3.7；available!==true 时由注册表整卡隐藏——与 Docker 卡同款谓词）
 * 数据面：WP-4 daemon 慢刷经 bridge 契约 quartz.bandix（WP-8 P2 链路）
 * 2026-10-17 用户指令：删 WAN 速率合计行（与"网络"卡重复）；加总体限速策略；
 * 设备表显示各自速率与限速策略标记。 */
import { computed } from 'vue';
import { Dot, EmptyState } from '../primitives.js';
import { fmtRate, fmtBytes, fmtPct } from '../../lib/format.js';

const props = defineProps({ payload: { type: Object, required: true }, host: { type: Object, default: () => ({}) } });
const t = (s) => (props.host.t ? props.host.t(s) : s);
const b = computed(() => props.payload.bandix);
const top = computed(() => (b.value && b.value.top) || []);
const lim = computed(() => (b.value && b.value.limits) || null);
const name = (it) => it.host || it.ip || it.mac;

/* 设备的限速策略标记（紧凑；完整规则在网页 bandix 页）：
 * 白名单 > 时段限速规则 > 配额（三者可叠加，按此优先级显示首个） */
const policyOf = (it) => {
	if (it.wl) return t('Whitelisted');
	if (it.sch) return t('Limits:') + ' ' + it.sch;
	if (it.q) return t('Quota');
	return t('Default');
};
const policyTone = (it) => (it.wl ? 'ok' : it.sch ? 'warn' : it.q ? 'warn' : '');
</script>

<template>
	<template v-if="b">
		<template v-if="b.devices || top.length">
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

			<!-- 总体限速策略（默认限速 / 白名单开关 / 规则与配额计数） -->
			<div v-if="lim" class="qzd-kv">
				<span class="qzd-kv-k">{{ t('Default rate limit') }}</span>
				<span class="qzd-kv-v">
					&darr; {{ lim.default_rx_bps ? fmtRate(lim.default_rx_bps) : t('unlimited') }}
					&middot; &uarr; {{ lim.default_tx_bps ? fmtRate(lim.default_tx_bps) : t('unlimited') }}
				</span>
			</div>
			<div v-if="lim" class="qzd-kv">
				<span class="qzd-kv-k">{{ t('Whitelist') }}</span>
				<span class="qzd-kv-v">
					{{ lim.whitelist_enabled ? t('On') : t('Off') }}
					<span class="qzd-muted">&middot; {{ t('rules:') }} {{ lim.schedule_count || 0 }} &middot; {{ t('quotas:') }} {{ lim.quota_count || 0 }} &middot; {{ t('devices:') }} {{ lim.whitelist_count || 0 }}</span>
				</span>
			</div>

			<table v-if="top.length" class="qzd-table">
				<thead>
					<tr>
						<th class="qzd-w-bd">{{ t('Device') }}</th>
						<th class="qzd-w-br">{{ t('Download') }}</th>
						<th class="qzd-w-br">{{ t('Upload') }}</th>
						<th class="qzd-w-bd">{{ t('Total') }}</th>
						<th class="qzd-w-bp">{{ t('Rate limit') }}</th>
					</tr>
				</thead>
				<tbody>
					<tr v-for="it in top" :key="it.mac">
						<td>{{ name(it) }}</td>
						<td class="qz-mono">{{ fmtRate(it.rx_r || 0) }}</td>
						<td class="qz-mono">{{ fmtRate(it.tx_r || 0) }}</td>
						<td class="qz-mono qzd-dimmer">{{ fmtBytes((it.rx_b || 0) + (it.tx_b || 0)) }}</td>
						<td class="qzd-dimmer">
							<Dot v-if="policyTone(it)" :state="policyTone(it)" /> {{ policyOf(it) }}
						</td>
					</tr>
				</tbody>
			</table>
			<div class="qzd-cap">{{ t('Bandix per-device traffic (WAN rates; totals & share of cumulative bytes; policy = whitelist/schedule/quota)') }}</div>
		</template>
		<EmptyState v-else :msg="t('No traffic data yet')" />
	</template>
	<EmptyState v-else :msg="t('Unavailable — collector did not return Bandix data')" />
</template>
