package daemon

import (
	"testing"
	"time"

	"github.com/quartzwrt/quartz-screen/internal/snapshot"
)

func TestApplyBandixRates(t *testing.T) {
	d := &daemon{}
	s1 := &snapshot.Snapshot{Bandix: &snapshot.Bandix{Top: []snapshot.BandixTop{
		{MAC: "aa", RxB: 1000, TxB: 500},
	}}}
	d.applyBandixRates(s1, time.Unix(100, 0))
	if s1.Bandix.Top[0].RxR != 0 || s1.Bandix.Top[0].TxR != 0 {
		t.Fatalf("first sample should stay 0: %+v", s1.Bandix.Top[0])
	}
	s2 := &snapshot.Snapshot{Bandix: &snapshot.Bandix{Top: []snapshot.BandixTop{
		{MAC: "aa", RxB: 2000, TxB: 500},
	}}}
	d.applyBandixRates(s2, time.Unix(110, 0))
	if s2.Bandix.Top[0].RxR != 100 || s2.Bandix.Top[0].TxR != 0 {
		t.Fatalf("rates = %+v, want rx=100 tx=0", s2.Bandix.Top[0])
	}
	s3 := &snapshot.Snapshot{Bandix: &snapshot.Bandix{Top: []snapshot.BandixTop{
		{MAC: "aa", RxB: 10, TxB: 1200},
	}}}
	d.applyBandixRates(s3, time.Unix(120, 0))
	if s3.Bandix.Top[0].RxR != 0 || s3.Bandix.Top[0].TxR != 70 {
		t.Fatalf("wrap rates = %+v, want rx=0 tx=70", s3.Bandix.Top[0])
	}
}
