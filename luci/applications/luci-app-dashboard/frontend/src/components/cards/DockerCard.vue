<script setup>
/* Docker 卡（2026-10-19 真机反馈轮）：引擎/驱动/根目录 + 容器/镜像/网络/卷
 * + 容器列表（名称/镜像/状态/详情）。数据面 = daemon docker 节（契约 §3.4 加法），
 * 卡片不自行取数。 */
import { computed } from 'vue';
import { Dot, Stat, EmptyState } from '../primitives.js';
import { fmtBytes } from '../../lib/format.js';

const props = defineProps({ payload: { type: Object, required: true }, host: { type: Object, default: () => ({}) } });
const t = (s) => (props.host.t ? props.host.t(s) : s);

const d = computed(() => props.payload.docker);
const cs = computed(() => (d.value && Array.isArray(d.value.containers) ? d.value.containers : []));
const tone = (state) => (state === 'running' ? 'ok' : state === 'paused' ? 'warn' : 'err');
</script>

<template>
	<template v-if="d && d.available !== false">
		<div class="qzd-docker-head">
			<Dot :state="d.available ? 'ok' : 'warn'" />
			<span>{{ d.available ? t('running') : t('stopped') }}</span>
			<span v-if="d.version" class="qzd-mono">{{ d.version }}</span>
			<span v-if="d.driver" class="qzd-muted">&middot; {{ d.driver }}</span>
			<span v-if="d.root_dir" class="qzd-muted qzd-docker-root">{{ d.root_dir }}</span>
		</div>

		<div class="qzd-statgrid">
			<Stat :k="t('Containers')" :v="d.running + ' / ' + d.total" />
			<Stat :k="t('Images')" :v="String(d.images)" />
			<Stat :k="t('Networks')" :v="String(d.networks || 0)" />
			<Stat :k="t('Volumes')" :v="String(d.volumes || 0)" />
		</div>

		<table v-if="cs.length" class="qzd-table qzd-docker-table">
			<thead>
				<tr>
					<th class="qzd-w-name">{{ t('Name') }}</th>
					<th class="qzd-w-image">{{ t('Image') }}</th>
					<th class="qzd-w-state">{{ t('State') }}</th>
					<th>{{ t('Status') }}</th>
				</tr>
			</thead>
			<tbody>
				<tr v-for="c in cs" :key="c.name">
					<td>{{ c.name }}</td>
					<td class="qz-mono qzd-dimmer">{{ c.image || '—' }}</td>
					<td><Dot :state="tone(c.state)" /> {{ c.state }}</td>
					<td class="qzd-dimmer">{{ c.status || '—' }}</td>
				</tr>
			</tbody>
		</table>
		<EmptyState v-else :msg="t('No containers')" />

		<div v-if="d.unhealthy && d.unhealthy.length" class="qzd-errchip">
			<Dot state="warn" /><span>{{ t('Unhealthy') }}: {{ d.unhealthy.join(', ') }}</span>
		</div>
		<div class="qzd-cap">
			{{ t('Docker engine') }} {{ d.version || '—' }} &middot; {{ t('Root') }} {{ d.root_dir || '—' }}
			<template v-if="d.cpus"> &middot; {{ d.cpus }} CPU</template>
			<template v-if="d.mem_total"> &middot; {{ fmtBytes(d.mem_total) }}</template>
		</div>
	</template>
	<EmptyState v-else :msg="t('Unavailable — collector did not return Docker data')" />
</template>
