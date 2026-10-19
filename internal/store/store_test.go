package store

import (
	"path/filepath"
	"testing"
	"time"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestInsertAndQueryRaw(t *testing.T) {
	s := openTemp(t)
	ts := int64(1795000000)
	if err := s.InsertBatch(ts, map[string]float64{"cpu.total": 42.6, "cpu.steal": 1.8}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertBatch(ts+10, map[string]float64{"cpu.total": 43.0}); err != nil {
		t.Fatal(err)
	}
	pts, err := s.Query("cpu.total", ts, ts+10, "raw")
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 {
		t.Fatalf("期望 2 点，实得 %d", len(pts))
	}
	if pts[0].Avg != 42.6 || pts[1].Avg != 43.0 {
		t.Fatalf("点位值错误: %+v", pts)
	}
	// 指标隔离：steal 只有 1 点
	pts2, _ := s.Query("cpu.steal", ts, ts+10, "raw")
	if len(pts2) != 1 {
		t.Fatalf("cpu.steal 应只有 1 点，实得 %d", len(pts2))
	}
}

func TestQueryUnknownLayer(t *testing.T) {
	s := openTemp(t)
	if _, err := s.Query("cpu.total", 0, 1, "5m"); err == nil {
		t.Fatal("未知层应报错")
	}
}

func TestQueryEmpty(t *testing.T) {
	s := openTemp(t)
	pts, err := s.Query("not.exist", 0, 999999999, "raw")
	if err != nil || len(pts) != 0 {
		t.Fatalf("未知指标应返回空: %v %v", pts, err)
	}
}

func TestDownsampleRawTo1m(t *testing.T) {
	s := openTemp(t)
	// 固定 now：12:00:00 → cutoff = 11:59:00（聚合 < 11:59 的完整分钟桶）
	now := time.Unix(1795003200, 0) // 任意整点
	base := now.Unix() - 300        // 5 分钟前起
	// 在 [base, base+120) 内每分钟塞 3 个点
	for i := int64(0); i < 120; i += 10 {
		if err := s.InsertBatch(base+i, map[string]float64{"cpu.total": float64(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Downsample(now); err != nil {
		t.Fatal(err)
	}
	pts, err := s.Query("cpu.total", base, base+120, "1m")
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 {
		t.Fatalf("期望 2 个 1m 桶，实得 %d: %+v", len(pts), pts)
	}
	// 第一桶 [0,60)：值 0,10,20,30,40,50 → avg 25, max 50, min 0
	if pts[0].Avg != 25 || pts[0].Max != 50 || pts[0].Min != 0 {
		t.Fatalf("第一桶聚合错误: %+v", pts[0])
	}
	if pts[0].Ts != base-base%60 {
		t.Fatalf("桶起始时间错误: %d", pts[0].Ts)
	}

	// 幂等：再跑一次不应产生重复
	if err := s.Downsample(now); err != nil {
		t.Fatal(err)
	}
	pts, _ = s.Query("cpu.total", base, base+120, "1m")
	if len(pts) != 2 {
		t.Fatalf("重复降采样后应为 2 桶，实得 %d", len(pts))
	}
}

func TestDownsample1mTo10m(t *testing.T) {
	s := openTemp(t)
	now := time.Unix(1795003200, 0)
	base := (now.Unix() - 3600) / 60 * 60 // 整分钟起点，1 小时前
	// 造 20 分钟 raw 数据（每分钟 1 点，值为分钟序号），一次 Downsample 走完整链路 raw→1m→10m
	for i := int64(0); i < 20; i++ {
		if err := s.InsertBatch(base+i*60, map[string]float64{"cpu.total": float64(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Downsample(now); err != nil {
		t.Fatal(err)
	}
	// 1m 层应完整
	pts1m, err := s.Query("cpu.total", base, base+1200, "1m")
	if err != nil {
		t.Fatal(err)
	}
	if len(pts1m) != 20 {
		t.Fatalf("期望 20 个 1m 桶，实得 %d", len(pts1m))
	}
	pts, err := s.Query("cpu.total", base, base+1200, "10m")
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 {
		t.Fatalf("期望 2 个 10m 桶，实得 %d", len(pts))
	}
	// 第一桶 i=0..9：avg 4.5, max 9, min 0
	if pts[0].Avg != 4.5 || pts[0].Max != 9 || pts[0].Min != 0 {
		t.Fatalf("10m 聚合错误: %+v", pts[0])
	}
}

func TestPruneRaw(t *testing.T) {
	s := openTemp(t)
	now := time.Now()
	old := now.Add(-3 * time.Hour).Unix()
	fresh := now.Add(-1 * time.Hour).Unix()
	if err := s.InsertBatch(old, map[string]float64{"cpu.total": 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertBatch(fresh, map[string]float64{"cpu.total": 2}); err != nil {
		t.Fatal(err)
	}
	if err := s.Downsample(now); err != nil {
		t.Fatal(err)
	}
	pts, _ := s.Query("cpu.total", old-10, fresh+10, "raw")
	// 3h 前的应被清掉（raw 保留 2h），1h 前的保留
	if len(pts) != 1 || pts[0].Avg != 2 {
		t.Fatalf("过期清理错误: %+v", pts)
	}
}

func TestInsertBatchEmpty(t *testing.T) {
	s := openTemp(t)
	if err := s.InsertBatch(123, map[string]float64{}); err != nil {
		t.Fatal("空批次应直接成功")
	}
}
