#
# quartz-wifi-drivers — 进阶层 Wi-Fi 驱动家族（WP-14，2026-11-21 用户拍板方案 B）
#
# 形态：每个家族一个空 meta 包（QuartzWrt → 2. Advanced），勾选后在包条目下展开
# 子选项，默认全选该家族驱动并 select 对应 kmod/固件包。select 而非 DEPENDS 的原因：
#   - metadata.pl 会把 meta `DEPENDS:=+kmod-x` 中 x 的 depends（含 @DRIVER_11*_SUPPORT）
#     上提到 meta → 能力符号无 prompt（hostapd/Config.in default n）→ 菜单整组隐藏；
#   - config 块里的 `select` 不检查依赖（metadata.pl 源码注释），因此由本块同时
#     select 能力符号（DRIVER_11AC/AX/BE），并用 depends on PACKAGE_<meta> 做勾选门控。
#
# 定位：面向手动 menuconfig/裁剪用户的**构建期**驱动选择；发布镜像的预装集合仍由
#   QuartzWrt-Builder config/preinstall/drivers.config 决定（两者互不影响）。本 meta
#   为空包、默认不进入任何镜像，因此不承诺随发布 feed 分发（运行时安装请逐包 apk add）。
#

include $(TOPDIR)/rules.mk

PKG_NAME:=quartz-wifi-drivers
PKG_VERSION:=0.1.0
PKG_RELEASE:=1
PKG_LICENSE:=Apache-2.0
PKG_MAINTAINER:=QuartzWrt

include $(INCLUDE_DIR)/package.mk

define Build/Compile
	true
endef

define Package/quartz-wifi-mt76
  SECTION:=meta
  CATEGORY:=QuartzWrt
  SUBMENU:=2. Advanced
  TITLE:=Wi-Fi drivers: MediaTek mt76 (PCIe/USB)
  URL:=https://github.com/quartzwrt/QuartzWrt-Packages
  PKGARCH:=all
endef

define Package/quartz-wifi-mt76/description
  MediaTek mt76 家族（AP 成熟度最好）：MT7921/MT7922 PCIe+USB、MT7925 WiFi7
  PCIe+USB、MT7663U、MT7601U、MT7610U、MT7612U。固件随驱动 select 带出。
  仅构建期选择（不预装）。
endef

define Package/quartz-wifi-mt76/config
	config PACKAGE_QUARTZ_WIFI_MT76_DRIVERS
		bool "MediaTek mt76 (PCIe/USB)"
		depends on PACKAGE_quartz-wifi-mt76
		depends on PCI_SUPPORT || USB_SUPPORT
		default y
		select DRIVER_11AC_SUPPORT
		select DRIVER_11AX_SUPPORT
		select DRIVER_11BE_SUPPORT
		select PACKAGE_kmod-mt7921e
		select PACKAGE_kmod-mt7921u
		select PACKAGE_kmod-mt7925e
		select PACKAGE_kmod-mt7925u
		select PACKAGE_kmod-mt7663u
		select PACKAGE_kmod-mt7601u
		select PACKAGE_kmod-mt76x0u
		select PACKAGE_kmod-mt76x2u
endef

define Package/quartz-wifi-realtek
  SECTION:=meta
  CATEGORY:=QuartzWrt
  SUBMENU:=2. Advanced
  TITLE:=Wi-Fi drivers: Realtek rtw89/rtw88/rtl8xxxu
  URL:=https://github.com/quartzwrt/QuartzWrt-Packages
  PKGARCH:=all
endef

define Package/quartz-wifi-realtek/description
  Realtek 家族：rtw89（WiFi6/7 M.2 主力：RTL8852BE/CE、RTL8922AE）、
  rtw88（上一代 8822CE/BU、8821CU）与老 USB 棒 rtl8xxxu。固件随驱动带出。
  AP 成熟度以 STA 为主（适合上行/STA）。仅构建期选择（不预装）。
endef

define Package/quartz-wifi-realtek/config
	config PACKAGE_QUARTZ_WIFI_REALTEK_DRIVERS
		bool "Realtek rtw89 / rtw88 / rtl8xxxu"
		depends on PACKAGE_quartz-wifi-realtek
		depends on PCI_SUPPORT || USB_SUPPORT
		default y
		select DRIVER_11AC_SUPPORT
		select DRIVER_11AX_SUPPORT
		select DRIVER_11BE_SUPPORT
		select PACKAGE_kmod-rtw89-8852be
		select PACKAGE_kmod-rtw89-8852ce
		select PACKAGE_kmod-rtw89-8922ae
		select PACKAGE_kmod-rtw88-8822ce
		select PACKAGE_kmod-rtw88-8822bu
		select PACKAGE_kmod-rtw88-8821cu
		select PACKAGE_kmod-rtl8xxxu
endef

define Package/quartz-wifi-intel
  SECTION:=meta
  CATEGORY:=QuartzWrt
  SUBMENU:=2. Advanced
  TITLE:=Wi-Fi drivers: Intel iwlwifi (AX200/201/210/BE200)
  URL:=https://github.com/quartzwrt/QuartzWrt-Packages
  PKGARCH:=all
endef

define Package/quartz-wifi-intel/description
  Intel iwlwifi（PCIe/CNVi）：AX200、AX201、AX210、BE200 及对应固件。
  注意：6GHz/regdomain 受 LAR 限制。仅构建期选择（不预装）。
endef

define Package/quartz-wifi-intel/config
	config PACKAGE_QUARTZ_WIFI_INTEL_DRIVERS
		bool "Intel iwlwifi + AX200/AX201/AX210/BE200 firmware"
		depends on PACKAGE_quartz-wifi-intel
		depends on PCI_SUPPORT
		default y
		select DRIVER_11AC_SUPPORT
		select DRIVER_11AX_SUPPORT
		select DRIVER_11BE_SUPPORT
		select PACKAGE_kmod-iwlwifi
		select PACKAGE_iwlwifi-firmware-ax200
		select PACKAGE_iwlwifi-firmware-ax201
		select PACKAGE_iwlwifi-firmware-ax210
		select PACKAGE_iwlwifi-firmware-be200
endef

define Package/quartz-wifi-qualcomm
  SECTION:=meta
  CATEGORY:=QuartzWrt
  SUBMENU:=2. Advanced
  TITLE:=Wi-Fi drivers: Qualcomm ath11k/ath12k
  URL:=https://github.com/quartzwrt/QuartzWrt-Packages
  PKGARCH:=all
endef

define Package/quartz-wifi-qualcomm/description
  Qualcomm PCIe：ath11k（WiFi 6E，WCN6855/QCA2066 固件，约 12MB）与
  ath12k（WiFi 7，WCN7850 固件，约 8MB）。仅构建期选择（不预装）。
endef

define Package/quartz-wifi-qualcomm/config
	config PACKAGE_QUARTZ_WIFI_QUALCOMM_DRIVERS
		bool "Qualcomm ath11k + ath12k"
		depends on PACKAGE_quartz-wifi-qualcomm
		depends on PCI_SUPPORT
		default y
		select DRIVER_11AC_SUPPORT
		select DRIVER_11AX_SUPPORT
		select DRIVER_11BE_SUPPORT
		select PACKAGE_kmod-ath11k-pci
		select PACKAGE_ath11k-firmware-wcn6855
		select PACKAGE_ath11k-firmware-qca2066
		select PACKAGE_kmod-ath12k
		select PACKAGE_ath12k-firmware-wcn7850
endef

define Package/quartz-wifi-aic8800
  SECTION:=meta
  CATEGORY:=QuartzWrt
  SUBMENU:=2. Advanced
  TITLE:=Wi-Fi drivers: AIC8800 (USB/PCIe)
  URL:=https://github.com/quartzwrt/QuartzWrt-Packages
  PKGARCH:=all
endef

define Package/quartz-wifi-aic8800/description
  AIC8800（廉价 USB WiFi6 棒主力芯片；PCIe 形态同芯片）。固件随包投放。
  仅构建期选择（不预装）。
endef

define Package/quartz-wifi-aic8800/config
	config PACKAGE_QUARTZ_WIFI_AIC8800_DRIVERS
		bool "AIC8800 USB/PCIe"
		depends on PACKAGE_quartz-wifi-aic8800
		depends on PCI_SUPPORT || USB_SUPPORT
		default y
		select DRIVER_11AC_SUPPORT
		select DRIVER_11AX_SUPPORT
		select PACKAGE_kmod-aic8800-usb
		select PACKAGE_kmod-aic8800-pcie
endef

$(eval $(call BuildPackage,quartz-wifi-mt76))
$(eval $(call BuildPackage,quartz-wifi-realtek))
$(eval $(call BuildPackage,quartz-wifi-intel))
$(eval $(call BuildPackage,quartz-wifi-qualcomm))
$(eval $(call BuildPackage,quartz-wifi-aic8800))
