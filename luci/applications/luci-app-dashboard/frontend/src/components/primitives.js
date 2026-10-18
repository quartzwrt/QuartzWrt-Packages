/*
 * 通用小件（primitives，集中维护 —— 视觉硬规则在这里统一收口，DESIGN.md §5.3）
 * 卡片只允许组合这些件或经复核的自定义组件；不得自行取数。
 */
import { h, defineComponent, ref, watch, computed } from 'vue';
import { maskIp } from '../lib/format.js';

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

/* 地址值：默认打码（前半段 + *），点击/回车切换完整地址。
 * 展开状态只存在于当前组件实例，不写入任何存储；刷新页面即重新打码。 */
export const IpValue = defineComponent({
	props: {
		value: { type: String, default: '' },
		showLabel: { type: String, default: 'Click to reveal' },
		hideLabel: { type: String, default: 'Click to hide' }
	},
	setup(props) {
		const open = ref(false);
		const toggle = () => { if (props.value) open.value = !open.value; };
		return () => h('span', {
			class: ['qzd-ip', open.value ? 'qzd-ip-open' : ''],
			role: 'button',
			tabindex: props.value ? 0 : -1,
			title: open.value ? props.hideLabel : props.showLabel,
			onClick: (e) => { e.stopPropagation(); toggle(); },
			onKeydown: (e) => {
				if (e.key === 'Enter' || e.key === ' ') {
					e.preventDefault();
					toggle();
				}
			}
		}, open.value ? props.value : maskIp(props.value));
	}
});

/* 空态 / 不可用 */
export const EmptyState = (props) => h('div', { class: 'qzd-empty' }, props.msg);

/* 采集异常提示条（errors[] 的展示面，弱化提示） */
export const ErrorChip = (props) => h('div', { class: 'qzd-errchip' }, [
	Dot({ state: 'warn' }),
	h('span', {}, props.msg)
]);

/*
 * sparkline：手写 inline SVG（零图表库、无动画，DESIGN.md §5.2）
 * series 缺失时由客户端从轮询累积（契约 §5 消费者义务）。
 *
 * 平滑口径（IA v2 R3 定案，DESIGN.md §5.2）：EMA(α=0.3) → 等距降采样(~13 点)
 * → Catmull-Rom 转三次贝塞尔 → round linecap/linejoin。折线不再用 polyline
 * （尖角是视觉噪声），但仍是零依赖、无动画。
 *
 * values：单序列（如蜂窝 RSRP 历史）直接传入；fill=true 时画淡色面积。
 */
export const Sparkline = defineComponent({
	props: {
		series: { type: Object, default: null },
		rx: { type: Number, default: 0 },
		tx: { type: Number, default: 0 },
		values: { type: Array, default: null },
		fill: { type: Boolean, default: false },
		/* y 轴数值（顶部/中点/底部）；axisFormat 负责把原始值格式化成短标签 */
		axis: { type: Boolean, default: false },
		axisFormat: { type: Function, default: null },
		/* 行内小图的固定高度（px）；不传则沿用 .qzd-spark 的 66px */
		height: { type: [Number, String], default: null }
	},
	setup(props) {
		const buf = ref({ rx: [], tx: [] });

		watch(() => [props.rx, props.tx, props.series, props.values], () => {
			if (props.values && props.values.length) {
				buf.value = { rx: props.values.slice(-60), tx: [] };
				return;
			}
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

		/* 负值序列（蜂窝 RSRP）不能按 0..max 归一，否则曲线贴顶、面积铺满；
		 * 对含负值的序列用 min/max 加 15% 边距，正值序列仍保持 0..max。 */
		const domain = computed(() => {
			const all = buf.value.rx.concat(buf.value.tx);
			if (!all.length) return { min: 0, max: 1 };
			const lo = Math.min.apply(null, all);
			const hi = Math.max.apply(null, all);
			if (lo < 0) {
				const pad = (hi - lo || Math.abs(hi) || 1) * 0.15;
				return { min: lo - pad, max: hi + pad };
			}
			return { min: 0, max: hi * 1.15 || 1 };
		});

		const axisLabels = computed(() => {
			const { min, max } = domain.value;
			const fmt = props.axisFormat || ((v) => String(Math.round(v)));
			return [fmt(max), fmt((min + max) / 2), fmt(min)];
		});

		function ema(arr, alpha = 0.3) {
			if (!arr.length) return [];
			const out = [arr[0]];
			let prev = arr[0];
			for (let i = 1; i < arr.length; i++) {
				prev = prev + alpha * (arr[i] - prev);
				out.push(prev);
			}
			return out;
		}

		function downsample(arr, maxPoints = 13) {
			if (arr.length <= maxPoints) return arr.slice();
			const out = [];
			const step = (arr.length - 1) / (maxPoints - 1);
			for (let i = 0; i < maxPoints; i++)
				out.push(arr[Math.round(i * step)]);
			return out;
		}

		function pathOf(arr) {
			const smooth = downsample(ema(arr));
			if (smooth.length < 2) return '';
			const span = domain.value.max - domain.value.min || 1;
			const pts = smooth.map((v, i) => ({
				x: (i / (smooth.length - 1)) * 600,
				y: 63 - ((v - domain.value.min) / span) * 60
			}));
			let d = 'M' + pts[0].x.toFixed(1) + ',' + pts[0].y.toFixed(1);
			for (let i = 0; i < pts.length - 1; i++) {
				const p0 = pts[i - 1] || pts[i];
				const p1 = pts[i];
				const p2 = pts[i + 1];
				const p3 = pts[i + 2] || p2;
				d += ' C' + (p1.x + (p2.x - p0.x) / 6).toFixed(1) + ',' + (p1.y + (p2.y - p0.y) / 6).toFixed(1)
					+ ' ' + (p2.x - (p3.x - p1.x) / 6).toFixed(1) + ',' + (p2.y - (p3.y - p1.y) / 6).toFixed(1)
					+ ' ' + p2.x.toFixed(1) + ',' + p2.y.toFixed(1);
			}
			return d;
		}

		function areaOf(arr) {
			const d = pathOf(arr);
			if (!d) return '';
			return d + ' L600.0,66 L0.0,66 Z';
		}

		const single = () => !!(props.values && props.values.length);

		return () => {
			const svg = h('svg', {
				class: 'qzd-spark', viewBox: '0 0 600 66', preserveAspectRatio: 'none', 'aria-hidden': 'true',
				style: props.height
					? { height: typeof props.height === 'number' ? props.height + 'px' : props.height }
					: null
			}, [
				single()
					? (props.fill ? h('path', { d: areaOf(buf.value.rx), fill: 'var(--qz-ok)', opacity: '.10' }) : null)
					: h('path', {
						d: pathOf(buf.value.tx), fill: 'none', stroke: 'var(--qz-text-3)',
						'stroke-width': '1.4', 'stroke-linecap': 'round', 'stroke-linejoin': 'round'
					}),
				h('path', {
					d: pathOf(buf.value.rx), fill: 'none',
					stroke: single() ? 'var(--qz-ok)' : 'var(--qz-accent)',
					'stroke-width': single() ? '1.8' : '1.6',
					'stroke-linecap': 'round', 'stroke-linejoin': 'round'
				})
			]);

			if (!props.axis)
				return svg;

			/* 轴标签放在独立列里，不用 SVG <text>——preserveAspectRatio="none"
			 * 会把文字横向压扁；HTML 列还能跟随主题字体和行高。 */
			return h('div', { class: 'qzd-spark-axis' }, [
				h('div', { class: 'qzd-spark-ylabels' },
					axisLabels.value.map((label) => h('span', {}, label))),
				svg
			]);
		};
	}
});
