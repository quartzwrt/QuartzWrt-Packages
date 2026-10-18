<script setup>
/*
 * 页面骨架：数据四态（正常 / 采集器未运行 / 数据损坏 / 部分不可用由各卡降级）
 * 数据来源：host.readStatus()（壳层封装 rpcd file.read），轮询由 host.poll 接管
 */
import { ref, computed, onMounted, onUnmounted } from 'vue';
import Hero from './components/Hero.vue';
import CardShell from './components/CardShell.vue';
import cards from './cards';

const props = defineProps({ host: { type: Object, required: true } });
const host = props.host;
const t = (s) => (host.t ? host.t(s) : s);

const state = ref('loading');       // loading | normal | down | broken
const payload = ref(null);
const now = ref(Math.floor(Date.now() / 1000));

let agoTimer = null;

async function refresh() {
	try {
		payload.value = await host.readStatus();
		state.value = 'normal';
		now.value = Math.floor(Date.now() / 1000);
	} catch (e) {
		state.value = (e && e.kind === 'broken') ? 'broken' : 'down';
		payload.value = null;
	}
}

onMounted(() => {
	refresh();
	if (host.poll)
		host.poll(refresh, 5);
	agoTimer = setInterval(() => { now.value = Math.floor(Date.now() / 1000); }, 1000);
});

onUnmounted(() => { if (agoTimer) clearInterval(agoTimer); });

const visible = computed(() => {
	if (!payload.value)
		return [];
	const list = cards.filter((c) => !c.visible || c.visible(payload.value));
	// Docker 卡改为全宽容器表；无 Tailscale 时服务卡也占满一行，避免 4 列留白。
	const hasTailscale = list.some((c) => c.id === 'tailscale');
	return list.map((c) => c.id === 'services' ? { ...c, span: hasTailscale ? 8 : 12 } : c);
});

const agoText = computed(() => {
	const ts = payload.value && payload.value.ts;
	if (!ts) return '';
	const d = Math.max(0, now.value - ts);
	return d < 45 ? t('just now') : d < 3600 ? Math.round(d / 60) + ' ' + t('min ago') : Math.round(d / 3600) + ' ' + t('h ago');
});
</script>

<template>
	<div class="qzd-root">
		<!-- 采集器未运行 / 数据损坏：整页可读提示 + 骨架卡（不白屏，DESIGN.md §2.2） -->
		<div v-if="state === 'down'" class="alert-message error">
			<h4>{{ t('Collector is not running') }}</h4>
			<p>
				<span class="qz-mono">/var/run/quartz/status.json</span>
				{{ t('is missing — quartz-daemon is not started or not installed. Retrying automatically every 5 seconds.') }}
			</p>
		</div>
		<div v-else-if="state === 'broken'" class="alert-message error">
			<h4>{{ t('Collector data is corrupted') }}</h4>
			<p>{{ t('The status snapshot could not be parsed. Waiting for the next collection to overwrite it.') }}</p>
		</div>

		<template v-if="payload">
			<Hero :host-info="payload.host" :net="payload.net" :meta="payload" :ago="agoText" />
			<div class="qzd-grid">
				<CardShell v-for="c in visible" :key="c.id" :id="c.id" :span="c.span"
					:title="t(c.title)" :link="c.link" :link-label="c.linkLabel ? t(c.linkLabel) : null"
					:host="host">
					<component :is="c.component" :payload="payload" :host="host" />
				</CardShell>
			</div>
		</template>

		<!-- 骨架：数据未到（首屏）或采集器未运行 -->
		<div v-if="!payload" class="qzd-grid">
			<div v-for="n in 4" :key="n" class="qzd-card qzd-skel qzd-sp-6">
				<h4 class="qz:flex qz:items-center">
					<span class="qz:flex-1">&nbsp;</span>
					<span class="qzd-muted qz:text-xs">{{ t('Waiting for data') }}</span>
				</h4>
				<div class="qzd-skel-bar" style="width:62%"></div>
				<div class="qzd-skel-bar" style="width:84%"></div>
				<div class="qzd-skel-bar" style="width:47%"></div>
			</div>
		</div>
	</div>
</template>
