// Package fb 打开 Linux framebuffer 并写入 RGB565 帧。
package fb

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"github.com/quartzwrt/quartz-screen/internal/render"
)

const (
	fbioGetVarScreenInfo = 0x4600
	fbioGetFixScreenInfo = 0x4602
)

// Device 是一个已映射的 framebuffer。
type Device struct {
	Path   string
	Name   string
	W, H   int
	Stride int
	Bpp    int
	fd     int
	mem    []byte
	smem   int
}

// Info 返回可展示的信息。
func (d *Device) Info() string {
	return fmt.Sprintf("%s %dx%d %dbpp", d.Path, d.W, d.H, d.Bpp)
}

// Close 解除映射并关闭设备。
func (d *Device) Close() {
	if d.mem != nil {
		_ = syscall.Munmap(d.mem)
		d.mem = nil
	}
	if d.fd >= 0 {
		_ = syscall.Close(d.fd)
		d.fd = -1
	}
}

// Detect 按配置路径或 sysfs 名称选择 framebuffer。
func Detect(configured string) (string, error) {
	if configured != "" && configured != "auto" {
		if _, err := os.Stat(configured); err != nil {
			return "", err
		}
		return configured, nil
	}
	entries, err := filepath.Glob("/sys/class/graphics/fb*/name")
	if err != nil || len(entries) == 0 {
		return "", fmt.Errorf("no framebuffer found")
	}
	sort.Strings(entries)
	var candidates []string
	for _, namePath := range entries {
		raw, err := os.ReadFile(namePath)
		if err != nil {
			continue
		}
		name := string(raw)
		dev := "/dev/" + filepath.Base(filepath.Dir(namePath))
		switch {
		case strings.Contains(name, "gc9307"), strings.Contains(name, "fbtft"), strings.Contains(name, "fb_gc9307"):
			return dev, nil
		default:
			candidates = append(candidates, dev)
		}
	}
	if len(candidates) > 0 {
		return candidates[0], nil
	}
	return "", fmt.Errorf("no framebuffer found")
}

// Open 打开并映射 framebuffer。
func Open(path string) (*Device, error) {
	fd, err := syscall.Open(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	d := &Device{Path: path, fd: fd, Bpp: 16, W: render.ScreenW, H: render.ScreenH}
	var vbuf [256]byte
	if err := ioctl(fd, fbioGetVarScreenInfo, unsafe.Pointer(&vbuf[0])); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("FBIOGET_VSCREENINFO: %w", err)
	}
	d.W = int(binary.LittleEndian.Uint32(vbuf[0:4]))
	d.H = int(binary.LittleEndian.Uint32(vbuf[4:8]))
	d.Bpp = int(binary.LittleEndian.Uint32(vbuf[24:28]))
	var fbuf [128]byte
	if err := ioctl(fd, fbioGetFixScreenInfo, unsafe.Pointer(&fbuf[0])); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("FBIOGET_FSCREENINFO: %w", err)
	}
	d.Stride = int(binary.LittleEndian.Uint32(fbuf[48:52]))
	d.smem = int(binary.LittleEndian.Uint32(fbuf[24:28]))
	if d.Stride <= 0 {
		d.Stride = d.W * d.Bpp / 8
	}
	if d.smem < d.Stride*d.H {
		d.smem = d.Stride * d.H
	}
	if name, err := os.ReadFile("/sys/class/graphics/" + filepath.Base(path) + "/name"); err == nil {
		d.Name = string(name)
	}
	mem, err := syscall.Mmap(fd, 0, d.smem, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("mmap: %w", err)
	}
	d.mem = mem
	return d, nil
}

// Write 把画布写入 framebuffer；flip180 用于倒装。
func (d *Device) Write(c *render.Canvas, flip180 bool) error {
	if c.W != d.W || c.H != d.H {
		return fmt.Errorf("canvas %dx%d != fb %dx%d", c.W, c.H, d.W, d.H)
	}
	switch d.Bpp {
	case 16:
		for y := 0; y < d.H; y++ {
			srcY := y
			if flip180 {
				srcY = d.H - 1 - y
			}
			off := y * d.Stride
			for x := 0; x < d.W; x++ {
				srcX := x
				if flip180 {
					srcX = d.W - 1 - x
				}
				v := c.Pix[srcY*c.W+srcX]
				p := uint16((v>>19)&0x1F)<<11 | uint16((v>>10)&0x3F)<<5 | uint16((v>>3)&0x1F)
				binary.LittleEndian.PutUint16(d.mem[off+x*2:], p)
			}
		}
	case 32:
		for y := 0; y < d.H; y++ {
			srcY := y
			if flip180 {
				srcY = d.H - 1 - y
			}
			off := y * d.Stride
			for x := 0; x < d.W; x++ {
				srcX := x
				if flip180 {
					srcX = d.W - 1 - x
				}
				v := c.Pix[srcY*c.W+srcX]
				binary.LittleEndian.PutUint32(d.mem[off+x*4:], 0xFF000000|v)
			}
		}
	default:
		return fmt.Errorf("unsupported bpp %d", d.Bpp)
	}
	return nil
}

func ioctl(fd int, req uintptr, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}

// OpenFile 打开一个普通文件作为假 framebuffer（VM e2e / 预览验证用）。
func OpenFile(path string, w, h, bpp int) (*Device, error) {
	stride := w * bpp / 8
	size := stride * h
	fd, err := syscall.Open(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Ftruncate(fd, int64(size)); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	mem, err := syscall.Mmap(fd, 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		syscall.Close(fd)
		return nil, err
	}
	return &Device{Path: path, Name: "fake-fb", W: w, H: h, Stride: stride, Bpp: bpp, fd: fd, mem: mem, smem: size}, nil
}
