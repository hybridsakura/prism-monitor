package collect

import (
	"os"
	"strings"
	"testing"
)

func TestParseProcStatFixture(t *testing.T) {
	f, err := os.Open("testdata/proc/stat")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	agg, cores, err := parseProcStat(f)
	if err != nil {
		t.Fatal(err)
	}
	// fixture: user=1000 system=500 idle=8000 iowait=200 softirq=100 steal=50
	if agg.user != 1000 || agg.idle != 8000 || agg.steal != 50 {
		t.Fatalf("聚合行解析错误: %+v", agg)
	}
	if len(cores) != 2 {
		t.Fatalf("期望 2 核，实得 %d", len(cores))
	}
	if cores[0].steal != 25 {
		t.Fatalf("cpu0 steal 期望 25，实得 %v", cores[0].steal)
	}
	wantTotal := float64(1000 + 500 + 8000 + 200 + 100 + 50)
	if agg.total() != wantTotal {
		t.Fatalf("total 期望 %v，实得 %v", wantTotal, agg.total())
	}
}

func TestParseProcStatMissingAgg(t *testing.T) {
	_, _, err := parseProcStat(strings.NewReader("intr 123\nctxt 456\n"))
	if err == nil {
		t.Fatal("缺少 cpu 聚合行应报错")
	}
}

func TestCPUPercents(t *testing.T) {
	prev := cpuTimes{user: 1000, system: 500, idle: 8000, iowait: 200, softirq: 100, steal: 50}
	cur := cpuTimes{user: 1100, system: 550, idle: 8300, iowait: 250, softirq: 110, steal: 100}
	// totalD = 9850+? 计算：cur.total - prev.total
	// prev.total = 9850, cur.total = 1100+550+8300+250+110+100 = 10410, totalD=560
	// idleD = (8300+250)-(8000+200)=350 → usage = 100*(560-350)/560 = 37.5
	total, steal, iowait := cpuPercents(prev, cur)
	if total != 37.5 {
		t.Fatalf("usage 期望 37.5，实得 %v", total)
	}
	// stealD=50 → 100*50/560 ≈ 8.93
	if steal < 8.9 || steal > 9.0 {
		t.Fatalf("steal 期望约 8.93，实得 %v", steal)
	}
	// iowaitD=50 → 同 8.93
	if iowait < 8.9 || iowait > 9.0 {
		t.Fatalf("iowait 期望约 8.93，实得 %v", iowait)
	}
}

func TestCPUPercentsFirstSample(t *testing.T) {
	cur := cpuTimes{user: 100, idle: 900}
	total, steal, iowait := cpuPercents(cpuTimes{}, cur)
	if total != 0 || steal != 0 || iowait != 0 {
		t.Fatalf("首次采样应全 0，实得 %v/%v/%v", total, steal, iowait)
	}
}

func TestCPUPercentsCounterReset(t *testing.T) {
	prev := cpuTimes{user: 5000, idle: 9000}
	cur := cpuTimes{user: 10, idle: 90} // 回绕/重启
	total, _, _ := cpuPercents(prev, cur)
	if total != 0 {
		t.Fatalf("计数器回绕应返回 0，实得 %v", total)
	}
}

func TestParseLoadavg(t *testing.T) {
	l, err := parseLoadavg(strings.NewReader("1.24 0.98 0.76 2/345 12345\n"))
	if err != nil {
		t.Fatal(err)
	}
	if l[0] != 1.24 || l[1] != 0.98 || l[2] != 0.76 {
		t.Fatalf("loadavg 解析错误: %v", l)
	}
}

func TestClampPct(t *testing.T) {
	if clampPct(-1) != 0 || clampPct(101) != 100 || clampPct(50) != 50 {
		t.Fatal("clampPct 边界错误")
	}
}
