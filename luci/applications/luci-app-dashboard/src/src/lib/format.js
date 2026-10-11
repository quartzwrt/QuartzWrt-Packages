/* 格式化（技术数值一律等宽显示，单位见冻结契约 §3） */

export function fmtRate(bps) {
	const mbps = ((bps || 0) * 8) / 1e6;
	return (mbps >= 100 ? mbps.toFixed(0) : mbps.toFixed(1)) + ' Mbps';
}

export function fmtBytes(n) {
	if (n == null) return '—';
	if (n >= 1073741824) return (n / 1073741824).toFixed(n % 1073741824 ? 2 : 0) + ' GB';
	if (n >= 1048576) return (n / 1048576).toFixed(n % 1048576 ? 1 : 0) + ' MB';
	if (n >= 1024) return Math.round(n / 1024) + ' KB';
	return n + ' B';
}

export function fmtUptime(s) {
	const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
	return (d ? d + 'd ' : '') + ('0' + h).slice(-2) + ':' + ('0' + m).slice(-2);
}

export function fmtPct(v) {
	return v == null ? '—' : Number(v).toFixed(1) + '%';
}

export function fmtTemp(v) {
	return v == null ? '—' : Number(v).toFixed(1) + ' °C';
}

/* 相对时间（t = 壳层翻译函数） */
export function fmtAgo(sec, t) {
	const tr = t || ((x) => x);
	if (sec < 45) return tr('just now');
	if (sec < 3600) return Math.round(sec / 60) + ' ' + tr('min ago');
	if (sec < 86400) return Math.round(sec / 3600) + ' ' + tr('h ago');
	return Math.round(sec / 86400) + ' ' + tr('d ago');
}
