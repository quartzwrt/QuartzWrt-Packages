package collect

import "testing"

func TestParseContainers(t *testing.T) {
	running, total, unhealthy := parseContainers(readTestdata(t, "docker_containers.json"))
	if running != 3 {
		t.Fatalf("running = %d, want 3", running)
	}
	if total != 4 {
		t.Fatalf("total = %d, want 4", total)
	}
	if len(unhealthy) != 2 || unhealthy[0] != "nextcloud" || unhealthy[1] != "old-app" {
		t.Fatalf("unhealthy = %v", unhealthy)
	}
}

func TestParseContainerDigest(t *testing.T) {
	_, _, _, cs := parseContainerDigest(readTestdata(t, "docker_containers.json"))
	if len(cs) != 4 {
		t.Fatalf("containers = %d, want 4", len(cs))
	}
	if cs[0].State != "running" {
		t.Fatalf("running containers must be sorted first: %+v", cs[0])
	}
	for _, c := range cs {
		if c.Name == "" || c.State == "" {
			t.Fatalf("container summary missing fields: %+v", c)
		}
	}
}

func TestParseContainersCap(t *testing.T) {
	raw := []byte(`[`)
	for i := 0; i < 12; i++ {
		if i > 0 {
			raw = append(raw, ',')
		}
		raw = append(raw, []byte(`{"Names":["/c"]`)...)
		raw = append(raw, []byte(`,"State":"running","Status":"Up 1 min (unhealthy)"}`)...)
	}
	raw = append(raw, ']')
	_, _, unhealthy := parseContainers(raw)
	if len(unhealthy) != 8 {
		t.Fatalf("unhealthy = %d, want capped 8", len(unhealthy))
	}
}

func TestParseContainersBadJSON(t *testing.T) {
	running, total, unhealthy := parseContainers([]byte(`{"not":"an array"}`))
	if running != 0 || total != 0 || len(unhealthy) != 0 {
		t.Fatalf("bad json should yield zeroes, got %d %d %v", running, total, unhealthy)
	}
}
