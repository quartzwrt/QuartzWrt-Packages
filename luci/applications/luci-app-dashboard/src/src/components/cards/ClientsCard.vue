<script setup>
/* 在线设备卡：主机名 / IP / MAC / 接口 / 最近活跃（契约 §3.5；窄屏由 CSS 隐藏次要素列） */
import { computed } from 'vue';
import { EmptyState, ErrorChip } from '../primitives.js';
import { fmtAgo } from '../../lib/format.js';

const props = defineProps({ payload: { type: Object, required: true }, host: { type: Object, default: () => ({}) } });
const t = (s) => (props.host.t ? props.host.t(s) : s);
const c = computed(() => props.payload.clients);
const errors = computed(() => (props.payload.errors || []).filter((e) => /dhcp|lease/i.test(e)));
const agoOf = (it) => fmtAgo(Math.max(0, (props.payload.ts || 0) - it.last_seen), t);
</script>

<template>
	<template v-if="c && c.items && c.items.length">
		<table class="qzd-table">
			<thead>
				<tr>
					<th class="qzd-w-host">{{ t('Hostname') }}</th>
					<th class="qzd-w-ip">{{ t('IP address') }}</th>
					<th class="qzd-w-mac">{{ t('MAC address') }}</th>
					<th class="qzd-w-if">{{ t('Interface') }}</th>
					<th class="qzd-w-ago">{{ t('Last seen') }}</th>
				</tr>
			</thead>
			<tbody>
				<tr v-for="it in c.items" :key="it.mac">
					<td>{{ it.host || '—' }}</td>
					<td class="qz-mono">{{ it.ip }}</td>
					<td class="qz-mono qzd-dimmer">{{ it.mac }}</td>
					<td class="qzd-dimmer">{{ it.iface }}</td>
					<td class="qzd-dimmer">{{ agoOf(it) }}</td>
				</tr>
			</tbody>
		</table>
		<div class="qzd-cap">{{ t('Devices online:') }} {{ c.total }} &middot; {{ t('top') }} {{ c.items.length }}</div>
		<ErrorChip v-if="errors.length" :msg="t('Partial data unavailable:') + ' ' + errors.join('; ')" />
	</template>
	<template v-else>
		<EmptyState :msg="t('No devices online (DHCP leases unreadable)')" />
		<ErrorChip v-if="errors.length" :msg="errors.join('; ')" />
	</template>
</template>
