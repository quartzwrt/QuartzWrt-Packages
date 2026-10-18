package collect

import "testing"

func TestParseDefaultRoute(t *testing.T) {
	// VM 实机样张：仅 docker0/br-lan 网段路由，无默认路由
	if got := parseDefaultRoute(readTestdata(t, "proc_route.txt")); got != "" {
		t.Fatalf("VM fixture should have no default route, got %q", got)
	}

	// 合成：两条默认路由取 Metric 最小（hex）
	in := []byte("Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
		"eth0\t00000000\t0101A8C0\t0003\t0\t0\t0\t00000000\t0\t0\t0\n" +
		"eth1\t00000000\t0201A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n")
	if got := parseDefaultRoute(in); got != "eth0" {
		t.Fatalf("min-metric route = %q, want eth0", got)
	}

	// 无 up 标志（0000）→ 视为非默认路由
	down := []byte("Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
		"eth0\t00000000\t0101A8C0\t0000\t0\t0\t0\t00000000\t0\t0\t0\n")
	if got := parseDefaultRoute(down); got != "" {
		t.Fatalf("down route = %q, want empty", got)
	}
}

func TestParseIPv6GUA(t *testing.T) {
	in := `2: wwan0_1    inet6 fe80::1/64 scope link
2: wwan0_1    inet6 fd5b:d858:a07b::1/64 scope global
2: wwan0_1    inet6 2408:8459:1930:4e9a:549c:1ef9:780e:6166/64 scope global dynamic
2: wwan0_1    inet6 2408:8459:1930:4e9a:aaaa:bbbb:cccc:dddd/64 scope global temporary dynamic
`
	if got := parseIPv6GUA(in); got != "2408:8459:1930:4e9a:549c:1ef9:780e:6166" {
		t.Fatalf("GUA = %q", got)
	}
	if got := parseIPv6GUA("2: br-lan inet6 fe80::1/64 scope link\n"); got != "" {
		t.Fatalf("link-local must be skipped: %q", got)
	}
}

func TestDeltaBps(t *testing.T) {
	if got := deltaBps(1000, 3000, 2.0); got != 1000 {
		t.Fatalf("bps = %d, want 1000", got)
	}
	if got := deltaBps(5000, 100, 2.0); got != 0 {
		t.Fatalf("wrap bps = %d, want 0", got)
	}
	if got := deltaBps(0, 100, 0); got != 0 {
		t.Fatalf("zero-dt bps = %d, want 0", got)
	}
}
