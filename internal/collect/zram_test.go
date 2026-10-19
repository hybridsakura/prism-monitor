package collect

import (
	"os"
	"strings"
	"testing"
)

func TestParseMMStatFixture(t *testing.T) {
	f, err := os.Open("testdata/sys/block/zram0/mm_stat")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	mm, err := parseMMStat(f)
	if err != nil {
		t.Fatal(err)
	}
	if mm.origDataSize != 1782579200 || mm.comprDataSize != 626688000 || mm.memUsedTotal != 650000000 {
		t.Fatalf("mm_stat 解析错误: %+v", mm)
	}
}

func TestParseCompAlgorithm(t *testing.T) {
	f, err := os.Open("testdata/sys/block/zram0/comp_algorithm")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if algo := parseCompAlgorithm(f); algo != "zstd" {
		t.Fatalf("算法期望 zstd，实得 %q", algo)
	}
}

func TestParseCompAlgorithmNoBracket(t *testing.T) {
	if algo := parseCompAlgorithm(strings.NewReader("lzo\n")); algo != "lzo" {
		t.Fatalf("无括号时应退化取最后字段，实得 %q", algo)
	}
}

func TestZramStatsRatio(t *testing.T) {
	z := zramStats("zram0", zramMMStat{origDataSize: 1782579200, comprDataSize: 626688000, memUsedTotal: 650000000}, 2147483648, "zstd")
	// 1782579200 / 626688000 ≈ 2.844
	if z.Ratio < 2.84 || z.Ratio > 2.85 {
		t.Fatalf("压缩率期望约 2.84，实得 %v", z.Ratio)
	}
	if z.Used != 650000000 || z.Total != 2147483648 || z.Algo != "zstd" {
		t.Fatalf("ZramStat 装配错误: %+v", z)
	}
}

func TestZramStatsZeroCompr(t *testing.T) {
	z := zramStats("zram0", zramMMStat{}, 100, "")
	if z.Ratio != 0 {
		t.Fatal("压缩后 0 时压缩率应为 0（防除零）")
	}
}

func TestCollectorReadZramFixture(t *testing.T) {
	c := New("testdata")
	zs := c.readZram()
	if len(zs) != 1 {
		t.Fatalf("期望 1 个 zram 设备，实得 %d", len(zs))
	}
	if zs[0].Dev != "zram0" || zs[0].Algo != "zstd" {
		t.Fatalf("zram 设备信息错误: %+v", zs[0])
	}
}
