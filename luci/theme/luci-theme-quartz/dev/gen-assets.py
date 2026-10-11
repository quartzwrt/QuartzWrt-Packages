#!/usr/bin/env python3
"""生成 luci-theme-quartz 的静态资源：石英噪点纹理（可平铺 PNG）。

用法:
    python3 dev/gen-assets.py

输出（写入 root/www/luci-static/quartz/）:
    noise-dark.png   暗色模式噪点（白色颗粒）
    noise-light.png  浅色模式噪点（黑色颗粒）

仅使用 Python 标准库（random/struct/zlib），无第三方依赖，可复现（固定随机种子）。
设计规范见 docs/DESIGN.md §4：极低对比度、可平铺、体积预算 <2KB。
"""
import os
import random
import struct
import zlib

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, '..', 'root', 'www', 'luci-static', 'quartz')

SIZE = 96          # 平铺块尺寸（像素）
SEED = 0x517A      # "QZ" 的十六进制彩蛋，固定种子保证可复现
DENSITY = 0.065    # 非零像素占比（控制体积与观感）


def chunk(tag: bytes, data: bytes) -> bytes:
    return (struct.pack('>I', len(data)) + tag + data
            + struct.pack('>I', zlib.crc32(tag + data) & 0xffffffff))


def write_png(path: str, width: int, height: int, rows: list) -> int:
    raw = b''.join(b'\x00' + bytes(r) for r in rows)   # filter type 0 (None)
    png = (b'\x89PNG\r\n\x1a\n'
           + chunk(b'IHDR', struct.pack('>IIBBBBB', width, height, 8, 6, 0, 0, 0))
           + chunk(b'IDAT', zlib.compress(raw, 9))
           + chunk(b'IEND', b''))
    with open(path, 'wb') as fh:
        fh.write(png)
    return len(png)


def gen_noise(path: str, rgb: tuple, alpha_choices: list, rare_alpha: int,
              rare_chance: float = 0.012, density: float = DENSITY) -> int:
    rnd = random.Random(SEED)
    r, g, b = rgb
    rows = []
    for _y in range(SIZE):
        row = bytearray()
        for _x in range(SIZE):
            if rnd.random() < density:
                a = rare_alpha if rnd.random() < rare_chance else rnd.choice(alpha_choices)
            else:
                a = 0
            row += bytes((r, g, b, a)) if a else b'\x00\x00\x00\x00'
        rows.append(row)
    return write_png(path, SIZE, SIZE, rows)


def main() -> None:
    os.makedirs(OUT, exist_ok=True)

    # 暗色：白色颗粒，叠在 #1c1c1e 上表现为细微砂粒
    d = gen_noise(os.path.join(OUT, 'noise-dark.png'),
                  rgb=(255, 255, 255), alpha_choices=[3, 5, 8], rare_alpha=14)
    # 浅色：黑色颗粒，叠在 #ececf0 上（须比暗色更弱，浅底上颗粒更易察觉）
    l = gen_noise(os.path.join(OUT, 'noise-light.png'),
                  rgb=(0, 0, 0), alpha_choices=[1, 2, 3], rare_alpha=5, density=0.045)

    print(f'noise-dark.png  {d:>6} bytes')
    print(f'noise-light.png {l:>6} bytes')


if __name__ == '__main__':
    main()
