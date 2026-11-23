package render

import (
	"embed"
	"fmt"
	"sync"
)

//go:embed icondata/*.bin
var iconFS embed.FS

// Icon 是一枚 RGB888 + 4bpp alpha 的小图标（运营商标识用）。
type Icon struct {
	W, H int
	RGB  []uint32
	A    []uint8 // 0..15
}

var (
	iconOnce sync.Once
	iconErr  error
	icons    = map[string]*Icon{}
)

func loadIcons() error {
	raw, err := iconFS.ReadFile("icondata/operators.bin")
	if err != nil {
		return err
	}
	if len(raw) < 6 || string(raw[:4]) != "QZI1" {
		return fmt.Errorf("bad magic")
	}
	if raw[4] != 1 {
		return fmt.Errorf("unsupported version %d", raw[4])
	}
	count := int(raw[5])
	off := 6
	for i := 0; i < count; i++ {
		if off >= len(raw) {
			return fmt.Errorf("truncated icon %d", i)
		}
		n := int(raw[off])
		off++
		if off+n+2 > len(raw) {
			return fmt.Errorf("truncated name %d", i)
		}
		name := string(raw[off : off+n])
		off += n
		w, h := int(raw[off]), int(raw[off+1])
		off += 2
		npix := w * h
		if off+npix*3+(npix+1)/2 > len(raw) {
			return fmt.Errorf("truncated bitmap %d", i)
		}
		ic := &Icon{W: w, H: h, RGB: make([]uint32, npix), A: make([]uint8, npix)}
		for j := 0; j < npix; j++ {
			ic.RGB[j] = uint32(raw[off+j*3])<<16 | uint32(raw[off+j*3+1])<<8 | uint32(raw[off+j*3+2])
		}
		off += npix * 3
		for j := 0; j < npix; j++ {
			b := raw[off+j/2]
			if j%2 == 0 {
				ic.A[j] = (b >> 4) & 0xF
			} else {
				ic.A[j] = b & 0xF
			}
		}
		off += (npix + 1) / 2
		icons[name] = ic
	}
	return nil
}

// LoadIcons 解析内置图标（幂等；由 LoadFonts 一并调用）。
func LoadIcons() error {
	iconOnce.Do(func() { iconErr = loadIcons() })
	return iconErr
}

// IconByName 返回已加载图标；不存在返回 nil。
func IconByName(name string) *Icon {
	return icons[name]
}

// DrawIcon 在 (x,y) 叠加图标；返回是否命中。
func (c *Canvas) DrawIcon(name string, x, y int) bool {
	ic := icons[name]
	if ic == nil {
		return false
	}
	for yy := 0; yy < ic.H; yy++ {
		for xx := 0; xx < ic.W; xx++ {
			i := yy*ic.W + xx
			if ic.A[i] == 0 {
				continue
			}
			c.Blend(x+xx, y+yy, ic.RGB[i], uint32(ic.A[i])*17)
		}
	}
	return true
}
