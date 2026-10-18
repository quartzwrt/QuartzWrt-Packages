<script setup>
/* 服务与核心卡：关键服务状态 + OpenClash 核心版本 + Bandix 流量监控 + daemon 自诊断
 * （契约 §3.6 + §2；Bandix 行 = WP-8 bridge 链路在展示层的体现，DESIGN §6.3-3） */
import { computed } from 'vue';
import { Dot, Badge, EmptyState } from '../primitives.js';

const props = defineProps({ payload: { type: Object, required: true }, host: { type: Object, default: () => ({}) } });
const t = (s) => (props.host.t ? props.host.t(s) : s);
const svc = computed(() => props.payload.services);
const meta = computed(() => props.payload);
const hasAny = computed(() => !!svc.value || (meta.value && meta.value.interval != null));

/* 深链：存在性判定（对应菜单未装则不给链接，DESIGN §6.3-1 不死链） */
function pageHref(page) {
	if (!props.host.hasPage || !props.host.hasPage(page))
		return null;
	return props.host.url ? props.host.url(page) : null;
}
const bandixHref = computed(() => pageHref('admin/network/bandix'));
const tailscaleHref = computed(() => pageHref('admin/vpn/tailscale'));
const qmodemHref = computed(() => pageHref('admin/modem/qmodem'));
</script>

<template>
	<EmptyState v-if="!hasAny" :msg="t('Unavailable')" />
	<div v-else class="qzd-svc">
		<!-- OpenClash（开放映射键；未装/未返回时显示不可用） -->
		<div>
			<div class="qzd-svc-name">
				<Dot :state="svc && svc.openclash ? (svc.openclash.running ? 'ok' : 'err') : ''" />OpenClash
			</div>
			<div class="qzd-stat-v qz-mono">
				{{ svc && svc.openclash && svc.openclash.core ? svc.openclash.core : t('Unavailable') }}
			</div>
			<div v-if="svc && svc.openclash && svc.openclash.core_update" class="qzd-svc-extra">
				<Badge tone="notice">{{ t('Core update available') }} {{ svc.openclash.core_update }}</Badge>
			</div>
		</div>

		<!-- Tailscale（WP-10 探针：version/dns_name/backend_state/peers_online extras） -->
		<div>
			<div class="qzd-svc-name">
				<Dot :state="svc && svc.tailscale ? (svc.tailscale.running ? 'ok' : 'err') : ''" />Tailscale
			</div>
			<div class="qzd-stat-v qz-mono">
				{{ svc && svc.tailscale ? (svc.tailscale.version || (svc.tailscale.running ? t('running') : t('stopped'))) : t('Not installed') }}
				<a v-if="tailscaleHref" class="qzd-svc-link" :href="tailscaleHref">{{ t('Details') }} <span class="qzd-arrow">&rarr;</span></a>
			</div>
		</div>

		<!-- Bandix 流量监控（WP-8 bridge 链路：探针 services.bandix 由 WP-7 投放） -->
		<div>
			<div class="qzd-svc-name">
				<Dot :state="svc && svc.bandix ? (svc.bandix.running ? 'ok' : 'err') : ''" />Bandix
			</div>
			<div class="qzd-stat-v qz-mono">
				{{ svc && svc.bandix ? (svc.bandix.core || t('running')) : t('Not installed') }}
				<a v-if="bandixHref" class="qzd-svc-link" :href="bandixHref">{{ t('Details') }} <span class="qzd-arrow">&rarr;</span></a>
			</div>
		</div>

		<!-- QModem（探针只给运行态；型号/固件/信号在蜂窝卡） -->
		<div>
			<div class="qzd-svc-name">
				<Dot :state="svc && svc.qmodem ? (svc.qmodem.running ? 'ok' : 'err') : ''" />QModem
			</div>
			<div class="qzd-stat-v qz-mono">
				{{ svc && svc.qmodem ? (svc.qmodem.running ? t('running') : t('stopped')) : t('Not installed') }}
				<a v-if="qmodemHref" class="qzd-svc-link" :href="qmodemHref">{{ t('Details') }} <span class="qzd-arrow">&rarr;</span></a>
			</div>
		</div>

		<!-- quartz-daemon 自诊断（契约顶层 interval / collect_ms） -->
		<div>
			<div class="qzd-svc-name"><Dot state="ok" />quartz-daemon</div>
			<div class="qzd-stat-v qz-mono">
				{{ t('collect') }} {{ meta.interval }}s<template v-if="meta.collect_ms != null"> &middot; {{ t('last') }} {{ meta.collect_ms }}ms</template>
			</div>
		</div>
	</div>
</template>
