package daemon

import (
	"fmt"
	"time"

	"github.com/quartzwrt/quartz-screen/internal/render"
)

// drawTest 画 bring-up / 校准用的测试图案：
// 顶部 8 段纯色条、灰阶、方向标记、中心十字与屏参文字。
func (d *daemon) drawTest(now time.Time) {
	c := d.canvas
	th := render.Dark
	c.Clear(0x000000)

	/* 边框先画：RoundRect 的填充会覆盖内部，若放在最后会擦掉色条/文字。 */
	c.RoundRect(0, 0, render.ScreenW, render.ScreenH, 0, 0x000000, 0xffffff)

	barW := render.ScreenW / 8
	colors := []uint32{
		0xff0000, 0x00ff00, 0x0000ff, 0xffffff,
		0x000000, 0xffff00, 0x00ffff, 0xff00ff,
	}
	names := []string{"R", "G", "B", "W", "K", "Y", "C", "M"}
	for i, col := range colors {
		c.FillRect(i*barW, 0, barW, 56, col)
	}
	for i := 0; i < 8; i++ {
		g := uint32(i * 36)
		col := g<<16 | g<<8 | g
		c.FillRect(i*barW, 56, barW, 14, col)
	}
	// 方向标记：顶部红条 + 左右文字。
	c.FillRect(0, 70, render.ScreenW, 1, 0xff0000)
	c.Text(render.FaceByName("mono13"), 6, 86, "TOP", 0xffffff)
	c.TextRight(render.FaceByName("mono13"), 314, 86, "TOP", 0xffffff)
	c.Text(render.FaceByName("mono13"), 6, 104, "LEFT", 0xffffff)
	c.TextRight(render.FaceByName("mono13"), 314, 104, "RIGHT", 0xffffff)
	c.TextCenter(render.FaceByName("mono13"), 160, 122, "CENTER", 0xffffff)
	// 中心十字
	c.HLine(0, render.ScreenW-1, render.ScreenH/2, 0x00ff00)
	c.VLine(render.ScreenW/2, 0, render.ScreenH-1, 0x00ff00)
	fbInfo := "no fb"
	if d.dev != nil {
		fbInfo = fmt.Sprintf("%s %dx%d %dbpp", d.dev.Path, d.dev.W, d.dev.H, d.dev.Bpp)
	}
	c.Text(render.FaceByName("mono13"), 6, 140, fbInfo, th.Text)
	c.Text(render.FaceByName("mono13"), 6, 154,
		fmt.Sprintf("rotate=%d  %s", d.cfg.Rotate, now.Format("15:04:05")), th.Text2)
	c.TextRight(render.FaceByName("mono13"), 314, 154, "QuartzWrt", th.Text3)
	// 色条名称
	for i, name := range names {
		c.TextCenter(render.FaceByName("ui11"), i*barW+barW/2, 68, name, 0x000000)
	}
}
