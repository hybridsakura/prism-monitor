package collect

import (
	"os"
	"testing"
)

func TestParseMountsFixture(t *testing.T) {
	f, err := os.Open("testdata/proc/mounts")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	entries := parseMounts(f)
	// overlay/tmpfs/proc 被过滤，剩 4 个
	if len(entries) != 4 {
		t.Fatalf("期望 4 个挂载点，实得 %d: %+v", len(entries), entries)
	}
	if entries[0].path != "/" || entries[0].fs != "ext4" {
		t.Fatalf("根挂载解析错误: %+v", entries[0])
	}
	if entries[1].dev != "/dev/sda1" || entries[1].path != "/data" {
		t.Fatalf("/data 解析错误: %+v", entries[1])
	}
}

func TestParseMountsDedup(t *testing.T) {
	// 同路径重复挂载只保留第一个
	f, err := os.Open("testdata/proc/mounts")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	entries := parseMounts(f)
	seen := map[string]int{}
	for _, e := range entries {
		seen[e.path]++
	}
	for p, n := range seen {
		if n > 1 {
			t.Fatalf("挂载点 %s 重复 %d 次", p, n)
		}
	}
}

func TestUnescapeMountPath(t *testing.T) {
	if got := unescapeMountPath(`/mnt/my\040disk`); got != "/mnt/my disk" {
		t.Fatalf("转义还原错误: %q", got)
	}
}

func TestMountStatWithStub(t *testing.T) {
	// 桩掉 statfs，验证容量计算与百分比
	old := statfsFunc
	defer func() { statfsFunc = old }()
	statfsFunc = func(path string) (uint64, uint64, error) {
		if path == "/data" {
			return 2000, 360, nil // total 2000, avail 360 → used 1640 → 82%
		}
		return 1000, 1000, nil
	}
	ms, ok := mountStat(mountEntry{dev: "/dev/sda1", path: "/data", fs: "xfs"})
	if !ok {
		t.Fatal("应成功")
	}
	if ms.Used != 1640 || ms.Pct != 82 {
		t.Fatalf("容量计算错误: %+v", ms)
	}
}

func TestMountStatZeroTotal(t *testing.T) {
	old := statfsFunc
	defer func() { statfsFunc = old }()
	statfsFunc = func(path string) (uint64, uint64, error) { return 0, 0, nil }
	if _, ok := mountStat(mountEntry{path: "/x"}); ok {
		t.Fatal("total=0 应返回 false")
	}
}

func TestEscapePath(t *testing.T) {
	cases := map[string]string{"/": "_root", "/data": "_data", "/var/lib": "_var_lib"}
	for in, want := range cases {
		if got := EscapePath(in); got != want {
			t.Fatalf("EscapePath(%q) 期望 %q，实得 %q", in, want, got)
		}
	}
}
