package collect

import (
	"testing"
	"time"
)

// TestCollectFixtureEndToEnd 用 testdata 作为根目录跑两次完整采集：
// 第一次无差量（全 0），第二次因 fixture 静态不变，速率仍应为 0（dt>0 但计数器相等）。
func TestCollectFixtureEndToEnd(t *testing.T) {
	c := New("testdata")

	s1, err := c.Collect()
	if err != nil {
		t.Fatal(err)
	}
	if s1.CPU.Total != 0 || s1.CPU.Steal != 0 {
		t.Fatalf("首次采集百分比应为 0: %+v", s1.CPU)
	}
	if len(s1.CPU.Cores) != 2 {
		t.Fatalf("期望 2 核，实得 %d", len(s1.CPU.Cores))
	}
	if s1.CPU.Load[0] != 1.24 {
		t.Fatalf("loadavg 错误: %v", s1.CPU.Load)
	}
	if s1.Mem.Pct != 62.5 {
		t.Fatalf("Mem.Pct 期望 62.5，实得 %v", s1.Mem.Pct)
	}
	if s1.Swap.Used != 1296000*1024 {
		t.Fatalf("Swap.Used 错误: %d", s1.Swap.Used)
	}
	if len(s1.Disks) != 4 {
		t.Fatalf("期望 4 块盘，实得 %d", len(s1.Disks))
	}
	if len(s1.Nets) != 2 {
		t.Fatalf("期望 2 块网卡，实得 %d", len(s1.Nets))
	}
	if len(s1.Zram) != 1 {
		t.Fatalf("期望 1 个 zram，实得 %d", len(s1.Zram))
	}
	if s1.Host.OS != "Debian GNU/Linux 12 (bookworm)" {
		t.Fatalf("OS 解析错误: %q", s1.Host.OS)
	}
	if s1.Uptime != 2246400 {
		t.Fatalf("Uptime 期望 2246400，实得 %d", s1.Uptime)
	}

	time.Sleep(5 * time.Millisecond)
	s2, err := c.Collect()
	if err != nil {
		t.Fatal(err)
	}
	// fixture 静态 → 所有差量指标为 0
	if s2.CPU.Total != 0 || s2.Swap.InBps != 0 || s2.Disks[0].ReadBps != 0 || s2.Nets[0].RxBps != 0 {
		t.Fatal("静态 fixture 第二次采集速率应为 0")
	}
}

// TestMetricsFlatten 验证快照摊平成时序指标的命名规则。
func TestMetricsFlatten(t *testing.T) {
	s := &Snapshot{
		CPU:    CPUStat{Total: 42.6, Steal: 1.8, Iowait: 0.9, Load: [3]float64{1.24, 0.98, 0.76}},
		Mem:    MemStat{Pct: 62.3, Used: 1028},
		Swap:   SwapStat{Pct: 31, InBps: 0, OutBps: 204800},
		Disks:  []DiskStat{{Dev: "sda", ReadBps: 1024, WriteBps: 512, AwaitMs: 1.8}},
		Nets:   []NetStat{{If: "eth0", RxBps: 4096, TxBps: 1024}},
		Mounts: []MountStat{{Path: "/data", Pct: 82}},
		Zram:   []ZramStat{{Dev: "zram0", Used: 626688, Ratio: 2.84}},
	}
	m := s.Metrics()
	checks := map[string]float64{
		"cpu.total": 42.6, "cpu.steal": 1.8, "cpu.iowait": 0.9, "cpu.load1": 1.24,
		"mem.pct": 62.3, "mem.used": 1028,
		"swap.pct": 31, "swap.out_bps": 204800,
		"disk.sda.read_bps": 1024, "disk.sda.write_bps": 512, "disk.sda.await_ms": 1.8,
		"net.eth0.rx_bps": 4096, "net.eth0.tx_bps": 1024,
		"mount._data.pct": 82,
		"zram.zram0.used":  626688, "zram.zram0.ratio": 2.84,
	}
	for k, want := range checks {
		if got, ok := m[k]; !ok || got != want {
			t.Fatalf("指标 %s 期望 %v，实得 %v (ok=%v)", k, want, got, ok)
		}
	}
}
