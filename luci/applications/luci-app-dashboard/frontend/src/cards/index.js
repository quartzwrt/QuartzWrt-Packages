/*
 * 卡片注册表（DESIGN.md §5.3）——新增卡 = 加一个模块 + 这里加一行
 * 字段：id（锚点/基线）/ title（msgid，经 po 翻译）/ span（12 列宽屏占位，配对原则"矮卡配矮卡"见 §5.1）
 *      link + linkLabel（深链，壳层做存在性判定）/ visible(payload)（可选，false 则整卡不渲染）
 * 硬规则：卡片不得自行取数；需要新数据先扩契约（§6.2）
 */
import SystemCard from '../components/cards/SystemCard.vue';
import NetworkCard from '../components/cards/NetworkCard.vue';
import DockerCard from '../components/cards/DockerCard.vue';
import ServicesCard from '../components/cards/ServicesCard.vue';
import ClientsCard from '../components/cards/ClientsCard.vue';

export default [
	{ id: 'system', title: 'System', span: 4, component: SystemCard },
	{
		id: 'network', title: 'Network', span: 8, component: NetworkCard,
		link: 'admin/status/realtime', linkLabel: 'Realtime traffic'
	},
	{
		id: 'docker', title: 'Docker', span: 4, component: DockerCard,
		link: 'admin/services/dockerman/containers', linkLabel: 'Containers',
		/* 契约 §3.4：available=false（未安装 dockerd）→ 整卡隐藏 */
		visible: (p) => !(p.docker && p.docker.available === false)
	},
	{ id: 'services', title: 'Services & cores', span: 8, component: ServicesCard },
	{
		id: 'clients', title: 'Clients', span: 12, component: ClientsCard,
		link: 'admin/status/routes', linkLabel: 'View all'
	}
];
