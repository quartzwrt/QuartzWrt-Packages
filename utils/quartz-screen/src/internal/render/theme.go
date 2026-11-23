package render

// 屏幕与栅格常量（NL68K 面板 172x320，横屏 320x172）。
const (
	ScreenW = 320
	ScreenH = 172

	Margin     = 6
	TopBarH    = 22
	ContentY   = Margin + TopBarH
	ContentW   = ScreenW - 2*Margin
	CardRadius = 10
)

// Theme 是 Quartz 视觉 token 在小屏上的 RGB 投影。
type Theme struct {
	Name         string
	Bg           uint32
	Panel        uint32
	Well         uint32
	Border       uint32
	BorderStrong uint32
	Text         uint32
	Text2        uint32
	Text3        uint32
	Accent       uint32
	OK           uint32
	Warn         uint32
	Err          uint32
}

var (
	// Dark 对应 luci-theme-quartz 暗色 token。
	Dark = &Theme{
		Name:         "dark",
		Bg:           0x1c1c1e,
		Panel:        0x2c2c2e,
		Well:         0x1c1c1e,
		Border:       0x3a3a3c,
		BorderStrong: 0x48484a,
		Text:         0xf2f2f2,
		Text2:        0xb3b3b3,
		Text3:        0x666666,
		Accent:       0x0a84ff,
		OK:           0x30d158,
		Warn:         0xff9f0a,
		Err:          0xff453a,
	}
	// Light 对应 luci-theme-quartz 浅色 token。
	Light = &Theme{
		Name:         "light",
		Bg:           0xececf0,
		Panel:        0xffffff,
		Well:         0xf2f2f7,
		Border:       0xebebeb,
		BorderStrong: 0xd9d9de,
		Text:         0x262626,
		Text2:        0x737373,
		Text3:        0xb3b3b3,
		Accent:       0x007aff,
		OK:           0x34c759,
		Warn:         0xff9500,
		Err:          0xff3b30,
	}
)

// ThemeByName 返回主题；未知名称回退暗色。
func ThemeByName(name string) *Theme {
	switch name {
	case "light":
		return Light
	default:
		return Dark
	}
}
