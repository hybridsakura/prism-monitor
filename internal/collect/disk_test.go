package collect

import (
	"os"
	"strings"
	"testing"
)

func TestParseDiskstatsFixture(t *testing.T) {
	f, err := os.Open("testdata/proc/diskstats")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ds, err := parseDiskstats(f)
	if err != nil {
		t.Fatal(err)
	}
	// loop0 与 ram0 应被过滤，剩 sda/sda1/nvme0n1/sdb
	if len(ds) != 4 {
		t.Fatalf("期望 4 个设备，实得 %d: %+v", len(ds), ds)
	}
	if ds[0].dev != "sda" || ds[0].sectorsRead != 5000000 || ds[0].msReading != 120000 {
		t.Fatalf("sda 解析错误: %+v", ds[0])
	}
}

func TestDiskRates(t *testing.T) {
	prev := diskCounters{dev: "sda", readsDone: 100000, sectorsRead: 5000000, msReading: 120000, writesDone: 50000, sectorsWr: 2500000, msWriting: 60000}
	cur := diskCounters{dev: "sda", readsDone: 100100, sectorsRead: 5200000, msReading: 120200, writesDone: 50050, sectorsWr: 2600000, msWriting: 60100}
	d := diskRates(prev, cur, 2.0)
	// 读：200000 扇区 × 512 / 2s = 51200000 B/s
	if d.ReadBps != 51200000 {
		t.Fatalf("ReadBps 期望 51200000，实得 %v", d.ReadBps)
	}
	// 写：100000 × 512 / 2 = 25600000
	if d.WriteBps != 25600000 {
		t.Fatalf("WriteBps 期望 25600000，实得 %v", d.WriteBps)
	}
	// IOPS：读 50/s，写 25/s
	if d.ReadIops != 50 || d.WriteIops != 25 {
		t.Fatalf("IOPS 错误: %v/%v", d.ReadIops, d.WriteIops)
	}
	// await：(200+100)ms / 150 ios = 2ms
	if d.AwaitMs != 2 {
		t.Fatalf("AwaitMs 期望 2，实得 %v", d.AwaitMs)
	}
}

func TestDiskRatesFirstSample(t *testing.T) {
	cur := diskCounters{dev: "sda", readsDone: 100, sectorsRead: 5000}
	d := diskRates(diskCounters{}, cur, 2.0)
	if d.ReadBps == 0 {
		// prev 为零值 → cur >= prev → 正常计算（不算回绕），这是允许的
	} else if d.ReadBps != 5000*512/2 {
		t.Fatalf("意外值: %v", d.ReadBps)
	}
	// 回绕场景必须归零
	reset := diskRates(diskCounters{dev: "sda", readsDone: 9999, sectorsRead: 9999}, cur, 2.0)
	if reset.ReadBps != 0 {
		t.Fatal("计数器回绕应返回 0")
	}
}

func TestSkipDisk(t *testing.T) {
	for _, dev := range []string{"loop0", "ram0", "sr0", "zram0", "fd0"} {
		if !skipDisk(dev) {
			t.Fatalf("%s 应被过滤", dev)
		}
	}
	for _, dev := range []string{"sda", "sda1", "nvme0n1", "nvme0n1p1", "vda", "mmcblk0"} {
		if skipDisk(dev) {
			t.Fatalf("%s 不应被过滤", dev)
		}
	}
}

func TestParseNetDevFixture(t *testing.T) {
	f, err := os.Open("testdata/proc/net/dev")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ns, err := parseNetDev(f)
	if err != nil {
		t.Fatal(err)
	}
	// lo 被跳过，剩 eth0/wlan0
	if len(ns) != 2 {
		t.Fatalf("期望 2 个网卡，实得 %d", len(ns))
	}
	if ns[0].iface != "eth0" || ns[0].rxBytes != 500000000 || ns[0].txBytes != 120000000 {
		t.Fatalf("eth0 解析错误: %+v", ns[0])
	}
}

func TestNetRates(t *testing.T) {
	prev := netCounters{iface: "eth0", rxBytes: 500000000, txBytes: 120000000}
	cur := netCounters{iface: "eth0", rxBytes: 500200000, txBytes: 120050000}
	n := netRates(prev, cur, 2.0)
	if n.RxBps != 100000 || n.TxBps != 25000 {
		t.Fatalf("速率错误: %v/%v", n.RxBps, n.TxBps)
	}
	// 回绕
	n2 := netRates(cur, prev, 2.0)
	if n2.RxBps != 0 {
		t.Fatal("计数器回绕应返回 0")
	}
}

func TestParseNetDevGarbage(t *testing.T) {
	ns, err := parseNetDev(strings.NewReader("garbage line without colon"))
	if err != nil || len(ns) != 0 {
		t.Fatalf("垃圾输入应容忍: %v %v", ns, err)
	}
}
