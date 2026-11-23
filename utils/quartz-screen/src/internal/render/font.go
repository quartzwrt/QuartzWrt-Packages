// Package render 提供小屏位图渲染：画布、字体、主题与组件。
package render

import (
	"embed"
	"encoding/binary"
	"fmt"
	"sync"
)

//go:embed fontdata/*.bin
var fontFS embed.FS

// Glyph 是单个字形的覆盖位图（0..15，4bpp 覆盖率）。
type Glyph struct {
	W, H               int
	BearingX, BearingY int
	Advance            int // 1/16 px
	Pix                []uint8
}

// Face 是一套固定字号的位图字体。
type Face struct {
	Name       string
	Ascent     int
	LineHeight int
	Glyphs     map[rune]*Glyph
}

func (f *Face) glyph(r rune) *Glyph {
	if f == nil {
		return nil
	}
	if g, ok := f.Glyphs[r]; ok {
		return g
	}
	return fallbackFace().Glyphs[r]
}

// TextWidth 返回字符串在当前字号下的像素宽度。
func (f *Face) TextWidth(s string) int {
	pen := 0
	for _, r := range s {
		if g := f.glyph(r); g != nil {
			pen += g.Advance
		} else {
			pen += f.Ascent << 4
		}
	}
	return (pen + 15) >> 4
}

var (
	fontOnce sync.Once
	fontErr  error
	faces    = map[string]*Face{}
)

var faceFiles = map[string]string{
	"ui9":        "fontdata/ui9.bin",
	"ui11":       "fontdata/ui11.bin",
	"ui13":       "fontdata/ui13.bin",
	"uib13":      "fontdata/uib13.bin",
	"uib16":      "fontdata/uib16.bin",
	"uib22":      "fontdata/uib22.bin",
	"uib28":      "fontdata/uib28.bin",
	"mono11":     "fontdata/mono11.bin",
	"mono13":     "fontdata/mono13.bin",
	"mono16":     "fontdata/mono16.bin",
	"mono22":     "fontdata/mono22.bin",
	"clock40":    "fontdata/clock40.bin",
	"clock64":    "fontdata/clock64.bin",
	"fallback14": "fontdata/fallback14.bin",
}

// LoadFonts 解析内置字体（幂等）。
func LoadFonts() error {
	fontOnce.Do(func() {
		for name, path := range faceFiles {
			f, err := loadFace(name, path)
			if err != nil {
				fontErr = fmt.Errorf("font %s: %w", name, err)
				return
			}
			faces[name] = f
		}
		if err := loadIcons(); err != nil {
			fontErr = fmt.Errorf("icons: %w", err)
		}
	})
	return fontErr
}

// FaceByName 返回已加载字体；未知名称回退到 ui13。
func FaceByName(name string) *Face {
	if f := faces[name]; f != nil {
		return f
	}
	return faces["ui13"]
}

func fallbackFace() *Face {
	if f := faces["fallback14"]; f != nil {
		return f
	}
	return &Face{Glyphs: map[rune]*Glyph{}}
}

func loadFace(name, path string) (*Face, error) {
	raw, err := fontFS.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) < 10 || string(raw[:4]) != "QZF1" {
		return nil, fmt.Errorf("bad magic")
	}
	if raw[4] != 1 {
		return nil, fmt.Errorf("unsupported version %d", raw[4])
	}
	bpp := int(raw[5])
	ascent := int(raw[6])
	lineHeight := int(raw[7])
	count := int(binary.LittleEndian.Uint16(raw[8:10]))
	f := &Face{Name: name, Ascent: ascent, LineHeight: lineHeight, Glyphs: make(map[rune]*Glyph, count)}
	off := 10
	for i := 0; i < count; i++ {
		if off+12 > len(raw) {
			return nil, fmt.Errorf("truncated glyph %d", i)
		}
		cp := rune(binary.LittleEndian.Uint32(raw[off:]))
		bx := int(int16(binary.LittleEndian.Uint16(raw[off+4:])))
		by := int(int16(binary.LittleEndian.Uint16(raw[off+6:])))
		adv := int(binary.LittleEndian.Uint16(raw[off+8:]))
		w := int(raw[off+10])
		h := int(raw[off+11])
		off += 12
		var npix int
		if bpp == 4 {
			npix = (w*h + 1) / 2
		} else {
			npix = ((w + 7) / 8) * h
		}
		if off+npix > len(raw) {
			return nil, fmt.Errorf("truncated bitmap %d", i)
		}
		pix := make([]uint8, w*h)
		if bpp == 4 {
			for j := 0; j < w*h; j++ {
				b := raw[off+j/2]
				if j%2 == 0 {
					pix[j] = (b >> 4) & 0xF
				} else {
					pix[j] = b & 0xF
				}
			}
		} else {
			stride := (w + 7) / 8
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					b := raw[off+y*stride+x/8]
					if b&(1<<(7-uint(x%8))) != 0 {
						pix[y*w+x] = 15
					}
				}
			}
		}
		off += npix
		f.Glyphs[cp] = &Glyph{W: w, H: h, BearingX: bx, BearingY: by, Advance: adv, Pix: pix}
	}
	return f, nil
}
