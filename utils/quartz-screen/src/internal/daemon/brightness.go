package daemon

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// applyBrightness 按日间/夜间配置写入背光；force 用于配置变更后强制刷新。
func (d *daemon) applyBrightness(force bool) {
	if d.dev == nil {
		return
	}
	pct := d.currentBrightness()
	night := d.cfg.InNightWindow(time.Now())
	if !force && pct == d.lastBri && night == d.lastNight {
		return
	}
	if err := d.setBrightness(pct); err != nil {
		if !d.briErr {
			d.addErr("backlight: " + err.Error())
			d.briErr = true
		}
	} else {
		d.briErr = false
	}
	d.lastBri = pct
	d.lastNight = night
}

func (d *daemon) setBrightness(pct int) error {
	br, mx := BacklightPath(d.cfg.Backlight)
	if br == "" {
		return fmt.Errorf("no backlight device")
	}
	maxv := readInt(mx, 255)
	if maxv <= 0 {
		maxv = 255
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	v := int(math.Round(float64(pct) * float64(maxv) / 100))
	if pct > 0 && v < 1 {
		v = 1
	}
	return os.WriteFile(br, []byte(strconv.Itoa(v)), 0o644)
}

func readInt(path string, def int) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		return def
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return def
	}
	return v
}
