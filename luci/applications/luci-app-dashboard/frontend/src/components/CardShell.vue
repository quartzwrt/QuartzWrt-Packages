<script setup>
/*
 * 卡片外壳：材质 + 标题 + 可选深链（存在性判定，不死链，DESIGN.md §6.3-1）
 * 单卡错误边界：一卡坏不坏整页（§5.3）
 */
import { ref, computed, onErrorCaptured } from 'vue';

const props = defineProps({
	id: { type: String, required: true },
	span: { type: Number, default: 4 },
	title: { type: String, required: true },
	link: { type: String, default: null },
	linkLabel: { type: String, default: null },
	host: { type: Object, default: () => ({}) }
});

const failed = ref(false);
onErrorCaptured(() => { failed.value = true; return false; });

const href = computed(() => {
	if (!props.link || !props.host.hasPage)
		return null;
	if (!props.host.hasPage(props.link))
		return null;
	return props.host.url ? props.host.url(props.link) : null;
});
</script>

<template>
	<div class="qzd-card" :id="id" :class="'qzd-sp-' + span">
		<h4>
			<span class="qz:flex-1">{{ title }}</span>
			<a v-if="href" class="qzd-more" :href="href">{{ linkLabel }} <span class="qzd-arrow">&rarr;</span></a>
		</h4>
		<div v-if="failed" class="qzd-empty">{{ $props.host.t ? host.t('Card failed to render') : 'Card failed to render' }}</div>
		<slot v-else />
	</div>
</template>
