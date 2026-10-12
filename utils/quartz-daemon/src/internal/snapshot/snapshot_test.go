package snapshot

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMarshalSmall(t *testing.T) {
	s := &Snapshot{V: 1, Interval: 2, Errors: []string{}}
	b, err := Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"errors":[]`) {
		t.Fatalf("errors must marshal as [] not null: %s", b)
	}
	if !strings.Contains(string(b), `"docker":null`) || !strings.Contains(string(b), `"services":null`) {
		t.Fatalf("optional sections must marshal as null: %s", b)
	}
	if !strings.Contains(string(b), `"v":1`) || !strings.Contains(string(b), `"interval":2`) {
		t.Fatalf("baseline fields missing: %s", b)
	}
}

func TestMarshalLadderEnforces32KB(t *testing.T) {
	s := &Snapshot{V: 1, Interval: 2, Errors: []string{}}
	items := make([]Client, 100)
	for i := range items {
		items[i] = Client{MAC: "aa:bb:cc:dd:ee:00", Host: strings.Repeat("x", 300), LastSeen: int64(i)}
	}
	s.Clients = &Clients{Total: 100, Items: items}
	s.Net.Series = Series{Step: 2, Rx: make([]int64, MaxSeries), Tx: make([]int64, MaxSeries)}

	b, err := Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > MaxBytes {
		t.Fatalf("size limit not enforced: %d bytes", len(b))
	}
	var out struct {
		Clients *Clients `json:"clients"`
		Errors  []string `json:"errors"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("output must stay valid JSON: %v", err)
	}
	if out.Clients != nil && len(out.Clients.Items) > MaxClients {
		t.Fatalf("clients not truncated: %d", len(out.Clients.Items))
	}
}

func TestSortClients(t *testing.T) {
	items := []Client{
		{MAC: "cc:00:00:00:00:00", LastSeen: 100},
		{MAC: "aa:00:00:00:00:00", LastSeen: 200},
		{MAC: "bb:00:00:00:00:00", LastSeen: 200},
	}
	SortClients(items)
	want := []string{"aa:00:00:00:00:00", "bb:00:00:00:00:00", "cc:00:00:00:00:00"}
	for i, mac := range want {
		if items[i].MAC != mac {
			t.Fatalf("order[%d] = %s, want %s", i, items[i].MAC, mac)
		}
	}
}
