package collect

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidateDescriptor(t *testing.T) {
	cases := []struct {
		name    string
		d       svcDesc
		wantErr bool
	}{
		{
			name: "valid procd+exec",
			d: svcDesc{
				ID:      "passwall",
				Running: &probeSpec{Type: "procd", Name: "passwall"},
				Extras: map[string]probeSpec{
					"core": {Type: "exec", Path: "/usr/bin/xray", Args: []string{"-version"}, Regex: `([0-9.]+)`},
				},
			},
		},
		{name: "bad id", d: svcDesc{ID: "Bad ID", Running: &probeSpec{Type: "procd", Name: "x"}}, wantErr: true},
		{name: "missing running", d: svcDesc{ID: "aa"}, wantErr: true},
		{name: "unknown type", d: svcDesc{ID: "aa", Running: &probeSpec{Type: "shell", Name: "x"}}, wantErr: true},
		{name: "relative exec path", d: svcDesc{ID: "aa", Running: &probeSpec{Type: "exec", Path: "bin/x"}}, wantErr: true},
		{name: "bad regex", d: svcDesc{ID: "aa", Running: &probeSpec{Type: "exec", Path: "/bin/x", Regex: "("}}, wantErr: true},
		{name: "pidof with slash", d: svcDesc{ID: "aa", Running: &probeSpec{Type: "pidof", Pattern: "/usr/bin/x"}}, wantErr: true},
		{name: "procd bad name", d: svcDesc{ID: "aa", Running: &probeSpec{Type: "procd", Name: "UPPER"}}, wantErr: true},
		{name: "cache out of range", d: svcDesc{ID: "aa", Running: &probeSpec{Type: "exec", Path: "/bin/x", Cache: 999999}}, wantErr: true},
	}
	for _, c := range cases {
		err := validateDescriptor(&c.d)
		if c.wantErr && err == nil {
			t.Fatalf("%s: expected error", c.name)
		}
		if !c.wantErr && err != nil {
			t.Fatalf("%s: unexpected error %v", c.name, err)
		}
	}
}

func TestLoadDescriptorsOverrideAndWarnings(t *testing.T) {
	share := t.TempDir()
	local := t.TempDir()

	write := func(dir, name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(share, "aa.json", `{"id":"aa","running":{"type":"procd","name":"aa"}}`)
	write(share, "cc.json", `{"id":"cc","title":"ShareCC","running":{"type":"procd","name":"cc"}}`)
	write(share, "bad.json", `{not json`)
	write(local, "cc.json", `{"id":"cc","title":"LocalCC","running":{"type":"procd","name":"cc"}}`)

	descs, warns := loadDescriptors([]string{share, local})
	if len(descs) != 2 {
		t.Fatalf("descs = %d, want 2", len(descs))
	}
	if descs["cc"] == nil || descs["cc"].Title != "LocalCC" {
		t.Fatalf("local override failed: %+v", descs["cc"])
	}
	if len(warns) != 1 {
		t.Fatalf("warns = %v, want 1 (bad.json)", warns)
	}
}

func TestBuildServices(t *testing.T) {
	initd := t.TempDir()
	if err := os.WriteFile(filepath.Join(initd, "testd"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	descs := map[string]*svcDesc{
		"testd": {
			ID:      "testd",
			Title:   "TestD",
			Running: &probeSpec{Type: "procd", Name: "testd"},
			Extras: map[string]probeSpec{
				"ver": {Type: "exec", Path: "/bin/echo", Args: []string{"v1.2.3"}, Regex: `v([0-9.]+)`},
			},
		},
		"missing": {ID: "missing", Running: &probeSpec{Type: "procd", Name: "missing"}},
	}

	// 运行中
	runner := &serviceRunner{
		svcList:   map[string]bool{"testd": true},
		execCache: map[string]execResult{},
		now:       time.Now,
		initdDir:  initd,
	}
	out := buildServices(descs, runner)
	if _, ok := out["missing"]; ok {
		t.Fatal("uninstalled service must be absent from services map")
	}
	v, ok := out["testd"].(map[string]any)
	if !ok {
		t.Fatalf("testd missing: %v", out)
	}
	if v["running"] != true || v["title"] != "TestD" || v["ver"] != "1.2.3" {
		t.Fatalf("testd value = %v", v)
	}

	// 已装但停止 → running:false
	runner2 := &serviceRunner{svcList: map[string]bool{}, execCache: map[string]execResult{}, now: time.Now, initdDir: initd}
	v2 := buildServices(descs, runner2)["testd"].(map[string]any)
	if v2["running"] != false {
		t.Fatalf("stopped value = %v", v2)
	}
}

func TestExtraValueCache(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "count")
	script := filepath.Join(dir, "probe.sh")
	os.WriteFile(script, []byte("#!/bin/sh\nc=$(cat \"$CNT\" 2>/dev/null || echo 0)\necho $((c+1)) > \"$CNT\"\necho v9.9.9\n"), 0o755)
	t.Setenv("CNT", counter)

	fake := time.Unix(1000, 0)
	runner := &serviceRunner{
		execCache: map[string]execResult{},
		now:       func() time.Time { return fake },
	}
	spec := &probeSpec{Type: "exec", Path: script, Regex: `v([0-9.]+)`, Cache: 300}

	if v, ok := runner.extraValue("aa", "ver", spec); !ok || v != "9.9.9" {
		t.Fatalf("first = %q ok=%v", v, ok)
	}
	if v, _ := runner.extraValue("aa", "ver", spec); v != "9.9.9" {
		t.Fatalf("cached = %q", v)
	}
	// 缓存期内应只执行过一次
	if b, _ := os.ReadFile(counter); string(b) != "1\n" {
		t.Fatalf("exec count = %q, want 1 (cached second call)", b)
	}
	// 过期后重新执行
	fake = fake.Add(301 * time.Second)
	runner.extraValue("aa", "ver", spec)
	if b, _ := os.ReadFile(counter); string(b) != "2\n" {
		t.Fatalf("exec count after expiry = %q, want 2", b)
	}
}

func TestExtraValueMissingPathOmitted(t *testing.T) {
	runner := &serviceRunner{execCache: map[string]execResult{}, now: time.Now}
	spec := &probeSpec{Type: "exec", Path: "/nonexistent/bin/xyz", Args: []string{"-v"}, Regex: `([0-9.]+)`}
	if _, ok := runner.extraValue("aa", "core", spec); ok {
		t.Fatal("missing exec path must yield ok=false (field omitted)")
	}
}
