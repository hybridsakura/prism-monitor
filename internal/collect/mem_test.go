package collect

import (
	"os"
	"testing"
)

func TestParseMeminfoFixture(t *testing.T) {
	f, err := os.Open("testdata/proc/meminfo")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m, err := parseMeminfo(f)
	if err != nil {
		t.Fatal(err)
	}
	if m.memTotal != 16384000*1024 {
		t.Fatalf("MemTotal 错误: %d", m.memTotal)
	}
	if m.memAvailable != 6144000*1024 {
		t.Fatalf("MemAvailable 错误: %d", m.memAvailable)
	}
	if m.swapCached != 96000*1024 {
		t.Fatalf("SwapCached 错误: %d", m.swapCached)
	}
}

func TestMemStats(t *testing.T) {
	m := meminfo{
		memTotal:      16384000 * 1024,
		memAvailable:  6144000 * 1024,
		buffers:       500000 * 1024,
		cached:        2000000 * 1024,
		swapCached:    96000 * 1024,
		swapTotal:     4096000 * 1024,
		swapFree:      2800000 * 1024,
	}
	mem, swap := memStats(m)
	wantUsed := uint64(10240000 * 1024)
	if mem.Used != wantUsed {
		t.Fatalf("mem.Used 期望 %d，实得 %d", wantUsed, mem.Used)
	}
	if mem.Pct != 62.5 {
		t.Fatalf("mem.Pct 期望 62.5，实得 %v", mem.Pct)
	}
	if mem.Cached != 2500000*1024 {
		t.Fatalf("mem.Cached 应为 cached+buffers: %d", mem.Cached)
	}
	if swap.Used != 1296000*1024 {
		t.Fatalf("swap.Used 错误: %d", swap.Used)
	}
	// 1296000/4096000 = 31.64%
	if swap.Pct < 31.6 || swap.Pct > 31.7 {
		t.Fatalf("swap.Pct 期望约 31.64，实得 %v", swap.Pct)
	}
	if swap.Cached != 96000*1024 {
		t.Fatalf("swap.Cached 错误: %d", swap.Cached)
	}
}

func TestMemStatsNoSwap(t *testing.T) {
	m := meminfo{memTotal: 1024, memAvailable: 512}
	_, swap := memStats(m)
	if swap.Pct != 0 || swap.Total != 0 {
		t.Fatalf("无 SWAP 机器应全 0: %+v", swap)
	}
}

func TestParseVmstat(t *testing.T) {
	f, err := os.Open("testdata/proc/vmstat")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	c, err := parseVmstat(f)
	if err != nil {
		t.Fatal(err)
	}
	if c.pswpin != 1200 || c.pswpout != 3400 {
		t.Fatalf("vmstat 解析错误: %+v", c)
	}
}

func TestSwapRates(t *testing.T) {
	prev := vmstatCounters{pswpin: 1200, pswpout: 3400}
	cur := vmstatCounters{pswpin: 1250, pswpout: 3500}
	in, out := swapRates(prev, cur, 2.0)
	// 50 页 × 4096 / 2s = 102400 B/s
	if in != 102400 {
		t.Fatalf("换入速率期望 102400，实得 %v", in)
	}
	// 100 页 × 4096 / 2s = 204800
	if out != 204800 {
		t.Fatalf("换出速率期望 204800，实得 %v", out)
	}
}

func TestSwapRatesReset(t *testing.T) {
	prev := vmstatCounters{pswpin: 5000, pswpout: 5000}
	cur := vmstatCounters{pswpin: 10, pswpout: 10}
	in, out := swapRates(prev, cur, 2.0)
	if in != 0 || out != 0 {
		t.Fatal("计数器回绕应返回 0")
	}
}
