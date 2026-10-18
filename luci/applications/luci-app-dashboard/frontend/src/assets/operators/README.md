# 运营商图标（仅符号部分）

本目录只保留三家中国运营商官方 logo 的**图形符号**，用于 Dashboard 蜂窝卡头部标识当前驻网运营商；不含文字商标。

| 文件 | 来源（Wikipedia 文件页） | 说明 |
| :--- | :--- | :--- |
| `china-mobile.svg` | [File:China_Mobile_Logo.svg](https://en.wikipedia.org/wiki/File:China_Mobile_Logo.svg) | 裁剪自官方 logo 的蓝/绿菱形符号 |
| `china-unicom.svg` | [File:China Unicom logo (2022).svg](https://en.wikipedia.org/wiki/File:China_Unicom_logo_(2022).svg) | 裁剪自官方 logo 的红色中国结符号 |
| `china-telecom.svg` | [File:China Telecom Logo.svg](https://en.wikipedia.org/wiki/File:China_Telecom_Logo.svg) | 裁剪自官方 logo 的蓝色 "e" 符号 |

**商标声明**：中国移动、中国联通、中国电信的 logo 为其各自权利人的商标。此处仅用于在路由器界面中标识当前移动网络运营商（nominal/identifying use），不表示任何隶属或背书关系。若需再分发本项目，请自行确认所在司法辖区的商标使用要求。

这些文件经裁剪/精简（viewBox + path），总大小约 5.4 KB，Vite 以 `?raw` 内联进 `app.js`，不产生额外静态资源文件。
