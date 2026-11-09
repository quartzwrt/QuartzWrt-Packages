package collect

import (
	"os"
	"path/filepath"
	"testing"
)

func writeBatteryFixture(t *testing.T, base string) {
	t.Helper()
	bat := filepath.Join(base, "pcat-battery")
	chg := filepath.Join(base, "pcat-charger")
	if err := os.MkdirAll(bat, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(chg, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(bat, "present"):      "1\n",
		filepath.Join(bat, "capacity"):     "82\n",
		filepath.Join(bat, "status"):       "Charging\n",
		filepath.Join(bat, "voltage_now"):  "3980000\n",
		filepath.Join(bat, "energy_now"):   "21200000\n",
		filepath.Join(bat, "energy_full"):  "25900000\n",
		filepath.Join(bat, "technology"):   "Li-ion\n",
		filepath.Join(bat, "manufacturer"): "Ariaboard\n",
		filepath.Join(chg, "online"):       "1\n",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadBatteryFrom(t *testing.T) {
	base := t.TempDir()
	writeBatteryFixture(t, base)

	got := readBatteryFrom(base)
	if !got.Available || !got.Present || !got.ChargerOnline {
		t.Fatalf("available/present/charger = %v/%v/%v, want true/true/true",
			got.Available, got.Present, got.ChargerOnline)
	}
	if got.Capacity != 82 {
		t.Fatalf("capacity = %d, want 82", got.Capacity)
	}
	if got.Status != "Charging" {
		t.Fatalf("status = %q, want Charging", got.Status)
	}
	if got.VoltageMV != 3980 {
		t.Fatalf("voltage_mv = %d, want 3980", got.VoltageMV)
	}
	if got.EnergyNowWh != 21.2 || got.EnergyFullWh != 25.9 {
		t.Fatalf("energy = %v/%v Wh, want 21.2/25.9", got.EnergyNowWh, got.EnergyFullWh)
	}
	if got.Technology != "Li-ion" || got.Manufacturer != "Ariaboard" {
		t.Fatalf("battery meta = %q/%q", got.Technology, got.Manufacturer)
	}
}

func TestReadBatteryUnavailable(t *testing.T) {
	got := readBatteryFrom(t.TempDir())
	if got.Available {
		t.Fatalf("available = true on empty sysfs, want false")
	}
}

func TestReadBatteryMissingCapacity(t *testing.T) {
	base := t.TempDir()
	writeBatteryFixture(t, base)
	if err := os.Remove(filepath.Join(base, "pcat-battery", "capacity")); err != nil {
		t.Fatal(err)
	}
	if got := readBatteryFrom(base); got.Available {
		t.Fatalf("available = true without capacity, want false")
	}
}

func TestReadBatteryClampAndDefaults(t *testing.T) {
	base := t.TempDir()
	writeBatteryFixture(t, base)
	bat := filepath.Join(base, "pcat-battery")
	os.WriteFile(filepath.Join(bat, "capacity"), []byte("150\n"), 0o644)
	os.WriteFile(filepath.Join(bat, "status"), []byte("\n"), 0o644)
	os.WriteFile(filepath.Join(bat, "energy_now"), []byte("bad\n"), 0o644)

	got := readBatteryFrom(base)
	if got.Capacity != 100 {
		t.Fatalf("capacity = %d, want clamp 100", got.Capacity)
	}
	if got.Status != "Unknown" {
		t.Fatalf("status = %q, want Unknown", got.Status)
	}
	if got.EnergyNowWh != 0 {
		t.Fatalf("energy_now = %v, want 0", got.EnergyNowWh)
	}
}
