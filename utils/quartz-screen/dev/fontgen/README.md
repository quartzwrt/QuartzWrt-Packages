# fontgen — quartz-screen 位图字体生成器

**开发机工具，非构建依赖**。生成结果已提交到
`src/internal/render/fontdata/*.bin`，路由器构建不需要 Python/Chromium。

## 依赖

- Python 3 + Playwright（Chromium）
- Noto Sans CJK SC / Noto Sans Mono CJK SC（SIL OFL 1.1）

## 用法

```sh
python3 gen_font.py
```

- UI 字体：Noto Sans CJK 子集 + ASCII（11/13/16/22px，4bpp 覆盖率）；
- 等宽：Noto Sans Mono CJK（13/16/22px）+ 40px 时钟数字；
- 兜底：GB2312 6763 字（14px，1bpp），用于任意 SSID/便签文本；
- 浏览器 4x 渲染 → 盒式降采样 → 二进制 atlas。

## 二进制格式（little endian）

```
magic "QZF1"
u8 version=1
u8 bpp (4|1)
u8 ascent
u8 line_height
u16 glyph_count
per glyph:
  u32 codepoint
  i16 bearing_x, i16 bearing_y
  u16 advance_1/16px
  u8 w, u8 h
  bitmap: ceil(w*h*bpp/8)
```

4bpp 两像素/字节（高 nibble 在前）；1bpp 行内 MSB 在前。空格等无墨字形以
`w=0,h=0` 记录，仅贡献 advance。

## 许可

字体数据由 Noto Sans CJK 位图化而来，随包附带
`src/internal/render/fontdata/OFL.txt`；修改/再分发请保留 OFL 声明。
