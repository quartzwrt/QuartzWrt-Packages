package render

import "testing"

func TestLoadFonts(t *testing.T) {
	if err := LoadFonts(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ui9", "ui11", "ui13", "uib13", "uib16", "uib22", "uib28", "mono11", "mono13", "mono16", "mono22", "clock40", "clock64", "fallback14"} {
		f := FaceByName(name)
		if f == nil || len(f.Glyphs) == 0 {
			t.Fatalf("face %s empty", name)
		}
	}
	f := FaceByName("ui13")
	for _, r := range "概A1:" {
		if g := f.glyph(r); g == nil {
			t.Fatalf("missing glyph %q", r)
		}
	}
	// 空格必须以 0 尺寸字形存在（只贡献 advance）。
	if g := f.glyph(' '); g == nil || g.W != 0 || g.H != 0 || g.Advance == 0 {
		t.Fatalf("space glyph = %+v", g)
	}
	// GB2312 兜底应覆盖常用汉字。
	if g := fallbackFace().glyph('鑫'); g == nil {
		t.Fatal("fallback missing 鑫")
	}
	w1 := f.TextWidth("网络")
	if w1 <= 0 {
		t.Fatalf("text width = %d", w1)
	}
}

func TestDrawTextAndRGB565(t *testing.T) {
	if err := LoadFonts(); err != nil {
		t.Fatal(err)
	}
	c := NewCanvas(64, 32)
	c.Clear(0x000000)
	c.Text(FaceByName("ui13"), 2, 20, "Ab 1", 0xffffff)
	nonzero := 0
	for _, v := range c.Pix {
		if v != 0 {
			nonzero++
		}
	}
	if nonzero == 0 {
		t.Fatal("no pixels drawn")
	}
	raw := c.RGB565(false)
	if len(raw) != 64*32*2 {
		t.Fatalf("rgb565 len = %d", len(raw))
	}
	flipped := c.RGB565(true)
	if len(flipped) != len(raw) {
		t.Fatal("flip changed length")
	}
}

func TestOperatorIcons(t *testing.T) {
	if err := LoadFonts(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"china-mobile", "china-unicom", "china-telecom"} {
		if ic := IconByName(name); ic == nil || ic.W != 16 || ic.H != 16 {
			t.Fatalf("icon %s = %+v", name, ic)
		}
	}
	c := NewCanvas(32, 32)
	if !c.DrawIcon("china-unicom", 4, 4) {
		t.Fatal("DrawIcon missed")
	}
	nonzero := 0
	for _, v := range c.Pix {
		if v != 0 {
			nonzero++
		}
	}
	if nonzero == 0 {
		t.Fatal("icon drew no pixels")
	}
}

func TestRoundRect(t *testing.T) {
	c := NewCanvas(100, 50)
	c.Clear(0x000000)
	c.Line(0, 0, 99, 49, 0xffffff)
	c.RoundRect(2, 2, 96, 46, 10, 0x222222, 0x444444)
	if c.Pix[25*100+50] != 0x222222 {
		t.Fatalf("fill = %06x", c.Pix[25*100+50])
	}
}
