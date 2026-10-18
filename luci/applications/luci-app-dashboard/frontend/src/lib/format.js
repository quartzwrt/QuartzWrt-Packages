/* 格式化（技术数值一律等宽显示，单位见冻结契约 §3） */

export function fmtRate(bps) {
	const bits = (bps || 0) * 8;
	/* 蜂窝空闲/低速时 <1 Mbps 显示 0.0 Mbps 会误导；Kbps 更诚实 */
	if (bits < 1e6) return (bits / 1e3).toFixed(bits < 1e4 ? 1 : 0) + ' Kbps';
	const mbps = bits / 1e6;
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

/* 地址打码：保留前半段，后半段整体替换为 *。
 * IPv4 保留前两段（192.168.*.*），IPv6 保留前两组（2408:8459:*:*:*:*:*:*）。
 * 点击展开由 primitives.js 的 IpValue 负责（纯前端切换，不发请求）。 */
export function maskIp(ip) {
	if (!ip) return '—';
	const addr = String(ip).split('/')[0];
	if (addr.indexOf(':') >= 0) {
		const p = addr.split(':').filter(Boolean);
		return p.length >= 2 ? p.slice(0, 2).join(':') + ':*:*:*:*:*:*' : addr;
	}
	const p = addr.split('.');
	return p.length === 4 ? p[0] + '.' + p[1] + '.*.*' : ip;
}

/* 频率（MHz → GHz，两位小数；0/缺失 → null） */
export function fmtMHz(mhz) {
	return mhz ? (Number(mhz) / 1000).toFixed(2) + ' GHz' : null;
}

/* 相对时间（t = 壳层翻译函数） */
export function fmtAgo(sec, t) {
	const tr = t || ((x) => x);
	if (sec < 45) return tr('just now');
	if (sec < 3600) return Math.round(sec / 60) + ' ' + tr('min ago');
	if (sec < 86400) return Math.round(sec / 3600) + ' ' + tr('h ago');
	return Math.round(sec / 86400) + ' ' + tr('d ago');
}
