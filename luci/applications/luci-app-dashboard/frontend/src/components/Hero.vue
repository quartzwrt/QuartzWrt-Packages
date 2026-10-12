<script setup>
/* Hero：品牌时刻（衬线主机名）+ 型号/版本/内核 + uptime / WAN / 采集时刻 */
import { Dot } from './primitives.js';
import { fmtUptime } from '../lib/format.js';

const props = defineProps({
	hostInfo: { type: Object, required: true },
	net: { type: Object, default: null },
	meta: { type: Object, default: null },
	ago: { type: String, default: '' }
});
</script>

<template>
	<div class="qzd-hero">
		<div>
			<div class="qzd-hostname">{{ hostInfo.hostname }}</div>
			<div class="qzd-meta">{{ hostInfo.model }} &middot; {{ hostInfo.release }} &middot; Linux {{ hostInfo.kernel }}</div>
		</div>
		<div class="qzd-summary">
			<div>
				<div class="k">Uptime</div>
				<div class="v">{{ fmtUptime(hostInfo.uptime) }}</div>
			</div>
			<div>
				<div class="k">WAN</div>
				<div class="v">
					<Dot :state="net && net.wan && net.wan.up ? 'ok' : 'err'" />
					{{ net && net.wan ? net.wan.ipv4 : '—' }}
				</div>
			</div>
			<div>
				<div class="k">Collected</div>
				<div class="v">{{ ago || '—' }}</div>
			</div>
		</div>
	</div>
</template>
