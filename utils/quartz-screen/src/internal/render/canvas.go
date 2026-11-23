package render

import (
	"bytes"
	"image"
	"image/png"
	"math"
	"os"
)

// Canvas 是一块 0xRRGGBB 的像素画布（无 alpha 通道）。
type Canvas struct {
	W, H int
	Pix  []uint32
}

// NewCanvas 创建画布并填充黑色。
func NewCanvas(w, h int) *Canvas {
	c := &Canvas{W: w, H: h, Pix: make([]uint32, w*h)}
	return c
}

// Clear 用纯色填充整块画布。
func (c *Canvas) Clear(col uint32) {
	for i := range c.Pix {
		c.Pix[i] = col
	}
}

// Blend 以 a/255 的不透明度把 col 叠加到 (x,y)。
func (c *Canvas) Blend(x, y int, col uint32, a uint32) {
	if x < 0 || y < 0 || x >= c.W || y >= c.H || a == 0 {
		return
	}
	if a >= 255 {
		c.Pix[y*c.W+x] = col
		return
	}
	dst := c.Pix[y*c.W+x]
	inv := 255 - a
	r := (uint32((col>>16)&0xFF)*a + uint32((dst>>16)&0xFF)*inv) / 255
	g := (uint32((col>>8)&0xFF)*a + uint32((dst>>8)&0xFF)*inv) / 255
	b := (uint32(col&0xFF)*a + uint32(dst&0xFF)*inv) / 255
	c.Pix[y*c.W+x] = (r << 16) | (g << 8) | b
}

// FillRect 填充矩形。
func (c *Canvas) FillRect(x, y, w, h int, col uint32) {
	if w <= 0 || h <= 0 {
		return
	}
	x0 := max(0, x)
	y0 := max(0, y)
	x1 := min(c.W, x+w)
	y1 := min(c.H, y+h)
	for yy := y0; yy < y1; yy++ {
		row := c.Pix[yy*c.W:]
		for xx := x0; xx < x1; xx++ {
			row[xx] = col
		}
	}
}

// HLine 画水平线（含端点）。
func (c *Canvas) HLine(x0, x1, y int, col uint32) {
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	c.FillRect(x0, y, x1-x0+1, 1, col)
}

// VLine 画垂直线（含端点）。
func (c *Canvas) VLine(x, y0, y1 int, col uint32) {
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	c.FillRect(x, y0, 1, y1-y0+1, col)
}

// Line 画 1px 直线（Bresenham）。
func (c *Canvas) Line(x0, y0, x1, y1 int, col uint32) {
	dx := abs(x1 - x0)
	dy := -abs(y1 - y0)
	sx, sy := -1, -1
	if x0 < x1 {
		sx = 1
	}
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for {
		c.Blend(x0, y0, col, 255)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

// RoundRect 画圆角矩形：先按描边色画出外轮廓，再用填充色画内层。
func (c *Canvas) RoundRect(x, y, w, h, r int, fill, border uint32) {
	if w <= 0 || h <= 0 {
		return
	}
	r = min(r, min(w/2, h/2))
	drawShape := func(ox, oy, ww, hh, rr int, col uint32) {
		if ww <= 0 || hh <= 0 {
			return
		}
		for yy := 0; yy < hh; yy++ {
			inset := 0
			if rr > 0 {
				if yy < rr {
					inset = rr - int(math.Sqrt(float64(rr*rr-(rr-yy)*(rr-yy))))
				} else if yy >= hh-rr {
					d := yy - (hh - rr - 1)
					inset = rr - int(math.Sqrt(float64(rr*rr-d*d)))
				}
			}
			c.HLine(ox+inset, ox+ww-1-inset, oy+yy, col)
		}
	}
	drawShape(x, y, w, h, r, border)
	if w > 2 && h > 2 {
		drawShape(x+1, y+1, w-2, h-2, max(0, r-1), fill)
	}
}

// Circle 画实心/空心圆。
func (c *Canvas) Circle(cx, cy, r int, col uint32, fill bool) {
	for dy := -r; dy <= r; dy++ {
		dx := int(math.Sqrt(float64(r*r - dy*dy)))
		if fill {
			c.HLine(cx-dx, cx+dx, cy+dy, col)
		} else {
			c.Blend(cx-dx, cy+dy, col, 255)
			c.Blend(cx+dx, cy+dy, col, 255)
			c.Blend(cx+dy, cy-dx, col, 255)
			c.Blend(cx+dy, cy+dx, col, 255)
		}
	}
}

// Text 从 (x, baseline) 开始绘制字符串，返回结束 pen 的 x。
func (c *Canvas) Text(f *Face, x, baseline int, s string, col uint32) int {
	pen := x << 4
	for _, r := range s {
		g := f.glyph(r)
		if g == nil {
			// 无字形：画一个占位方块并前进一个 em。
			c.FillRect(pen>>4, baseline-f.Ascent+2, max(2, f.Ascent-4), 2, col)
			pen += f.Ascent << 4
			continue
		}
		gx := (pen >> 4) + g.BearingX
		gy := baseline + g.BearingY
		for yy := 0; yy < g.H; yy++ {
			for xx := 0; xx < g.W; xx++ {
				cov := g.Pix[yy*g.W+xx]
				if cov == 0 {
					continue
				}
				c.Blend(gx+xx, gy+yy, col, uint32(cov)*17)
			}
		}
		pen += g.Advance
	}
	return pen >> 4
}

// TextRight 右对齐绘制。
func (c *Canvas) TextRight(f *Face, right, baseline int, s string, col uint32) int {
	return c.Text(f, right-f.TextWidth(s), baseline, s, col)
}

// TextCenter 以 center 为水平中心绘制。
func (c *Canvas) TextCenter(f *Face, center, baseline int, s string, col uint32) int {
	return c.Text(f, center-f.TextWidth(s)/2, baseline, s, col)
}

// RGB565 把画布转成 little-endian RGB565 帧缓冲字节。
func (c *Canvas) RGB565(flip180 bool) []byte {
	out := make([]byte, c.W*c.H*2)
	n := 0
	for i := 0; i < c.W*c.H; i++ {
		j := i
		if flip180 {
			j = c.W*c.H - 1 - i
		}
		v := c.Pix[j]
		r := (v >> 16) & 0xFF
		g := (v >> 8) & 0xFF
		b := v & 0xFF
		p := uint16(r>>3)<<11 | uint16(g>>2)<<5 | uint16(b>>3)
		out[n] = byte(p)
		out[n+1] = byte(p >> 8)
		n += 2
	}
	return out
}

// RGBA 返回 image.Image（预览用）。
func (c *Canvas) RGBA() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, c.W, c.H))
	for i, v := range c.Pix {
		img.Pix[i*4+0] = uint8(v >> 16)
		img.Pix[i*4+1] = uint8(v >> 8)
		img.Pix[i*4+2] = uint8(v)
		img.Pix[i*4+3] = 0xFF
	}
	return img
}

// PNG 编码为 PNG；scale>1 时用最近邻放大（保留像素观感）。
func (c *Canvas) PNG(scale int) ([]byte, error) {
	if scale <= 1 {
		var buf bytes.Buffer
		if err := png.Encode(&buf, c.RGBA()); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	img := image.NewRGBA(image.Rect(0, 0, c.W*scale, c.H*scale))
	for y := 0; y < c.H*scale; y++ {
		for x := 0; x < c.W*scale; x++ {
			v := c.Pix[(y/scale)*c.W+(x/scale)]
			o := (y*c.W*scale + x) * 4
			img.Pix[o+0] = uint8(v >> 16)
			img.Pix[o+1] = uint8(v >> 8)
			img.Pix[o+2] = uint8(v)
			img.Pix[o+3] = 0xFF
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// SavePNG 把画布写成 PNG 文件。
func (c *Canvas) SavePNG(path string, scale int) error {
	data, err := c.PNG(scale)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
