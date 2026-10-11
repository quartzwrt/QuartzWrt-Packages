/*
 * 通用小件（primitives，集中维护 —— 视觉硬规则在这里统一收口，DESIGN.md §5.3）
 * 卡片只允许组合这些件或经复核的自定义组件；不得自行取数。
 */
import { h, defineComponent, ref, watch, computed } from 'vue';

/* 状态圆点（彩色仅作细小状态标记，符合主题硬规则） */
export const Dot = (props) => h('span', {
	class: ['qzd-dot', props.state ? props.state : ''],
	style: props.style
});

/* 数值块：标签 + 等宽数值 */
export const Stat = (props, { slots }) => h('div', { class: 'qzd-stat' }, [
	h('div', { class: 'qzd-stat-k' }, props.k),
	h('div', { class: 'qzd-stat-v qz-mono' }, slots.default ? slots.default() : props.v)
]);

/* 键值行：标签（固定列宽）+ 可选进度条 + 等宽数值 */
export const KV = (props, { slots }) => h('div', { class: 'qzd-kv' }, [
	h('span', { class: 'qzd-kv-k' }, props.k),
	props.bar != null ? h('div', { class: 'cbi-progressbar' }, [h('div', { style: { width: Math.min(100, props.bar) + '%' } })]) : null,
	h('span', { class: ['qzd-kv-v', props.muted ? 'qzd-muted' : ''] }, slots.default ? slots.default() : props.v)
]);

/* 徽标（沿用主题 .label 语义色） */
export const Badge = (props, { slots }) => h('span', {
	class: ['label', props.tone ? props.tone : '']
}, slots.default ? slots.default() : props.v);

/* 空态 / 不可用 */
export const EmptyState = (props) => h('div', { class: 'qzd-empty' }, props.msg);

/* 采集异常提示条（errors[] 的展示面，弱化提示） */
export const ErrorChip = (props) => h('div', { class: 'qzd-errchip' }, [
	Dot({ state: 'warn' }),
	h('span', {}, props.msg)
]);

/*
 * sparkline：手写 inline SVG（零图表库、无动画，DESIGN.md §5.2）
 * series 缺失时由客户端从轮询累积（契约 §5 消费者义务）
 */
export const Sparkline = defineComponent({
	props: {
		series: { type: Object, default: null },
		rx: { type: Number, default: 0 },
		tx: { type: Number, default: 0 }
	},
	setup(props) {
		const buf = ref({ rx: [], tx: [] });

		watch(() => [props.rx, props.tx, props.series], () => {
			if (props.series && props.series.rx && props.series.rx.length) {
				buf.value = { rx: props.series.rx.slice(-60), tx: (props.series.tx || []).slice(-60) };
				return;
			}
			const b = buf.value;
			b.rx.push(props.rx || 0);
			b.tx.push(props.tx || 0);
			if (b.rx.length > 60) b.rx.shift();
			if (b.tx.length > 60) b.tx.shift();
		}, { immediate: true });

		const max = computed(() => {
			const all = buf.value.rx.concat(buf.value.tx);
			return Math.max.apply(null, all.length ? all : [1]) * 1.15 || 1;
		});

		function pts(arr) {
			if (arr.length < 2) return '';
			return arr.map((v, i) => {
				const x = (i / (arr.length - 1)) * 600;
				const y = 66 - (v / max.value) * 60 - 3;
				return x.toFixed(1) + ',' + y.toFixed(1);
			}).join(' ');
		}

		return () => h('svg', {
			class: 'qzd-spark', viewBox: '0 0 600 66', preserveAspectRatio: 'none', 'aria-hidden': 'true'
		}, [
			h('polyline', { fill: 'none', stroke: 'var(--qz-text-3)', 'stroke-width': '1.4', points: pts(buf.value.tx) }),
			h('polyline', { fill: 'none', stroke: 'var(--qz-accent)', 'stroke-width': '1.6', points: pts(buf.value.rx) })
		]);
	}
});
