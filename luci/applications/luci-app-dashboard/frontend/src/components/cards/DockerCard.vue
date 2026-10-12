<script setup>
/* Docker 卡：运行/总数/镜像/不健康（契约 §3.4；available=false 时由注册表整卡隐藏） */
import { computed } from 'vue';
import { KV, Dot, EmptyState } from '../primitives.js';

const props = defineProps({ payload: { type: Object, required: true }, host: { type: Object, default: () => ({}) } });
const t = (s) => (props.host.t ? props.host.t(s) : s);
const d = computed(() => props.payload.docker);
</script>

<template>
	<EmptyState v-if="!d" :msg="t('Unavailable — collector did not return Docker data')" />
	<template v-else>
		<KV :k="t('Running')" :v="d.running + ' / ' + d.total" />
		<KV :k="t('Images')" :v="String(d.images)" />
		<KV :k="t('Unhealthy')" :muted="!(d.unhealthy && d.unhealthy.length)">
			<template v-if="d.unhealthy && d.unhealthy.length">
				<Dot state="warn" /> {{ d.unhealthy.join(', ') }}
			</template>
			<template v-else>{{ t('none') }}</template>
		</KV>
	</template>
</template>
