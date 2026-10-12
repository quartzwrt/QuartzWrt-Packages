/*
 * luci-app-dashboard — 前端入口
 * 壳层契约：window.QuartzDashboard.mount(el, host)
 * host（v1 只读）：readStatus / poll / t / hasPage / url / version（docs/DESIGN.md §2.3）
 */
import { createApp } from 'vue';
import App from './App.vue';
import './styles/app.css';

export function mount(el, host) {
	const app = createApp(App, { host: host || {} });
	app.config.errorHandler = function (err, instance, info) {
		/* 兜底：单卡错误由 CardShell 的错误边界消化，仍在控制台留痕便于诊断 */
		console.error('[qz-dashboard]', info, err);
	};
	app.mount(el);
	return app;
}
