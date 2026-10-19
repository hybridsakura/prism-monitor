package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hybridsakura/prism-monitor/internal/collect"
	"github.com/hybridsakura/prism-monitor/internal/store"
)

func newTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	c := collect.New(filepath.Join("..", "collect", "testdata"))
	return New(c, st, nil), st
}

func sampleSnapshot() *collect.Snapshot {
	return &collect.Snapshot{
		Ts:     1795008000,
		Host:   collect.HostStat{Hostname: "prod-node-01", OS: "Debian GNU/Linux 12"},
		Uptime: 2246400,
		CPU:    collect.CPUStat{Total: 42.6, Steal: 1.8, Iowait: 0.9, Cores: []float64{40, 45}, Load: [3]float64{1.24, 0.98, 0.76}},
		Mem:    collect.MemStat{Total: 16 << 30, Used: 10 << 30, Pct: 62.5},
		Swap:   collect.SwapStat{Total: 4 << 30, Used: 1266 << 20, Pct: 31, OutBps: 204800},
		Mounts: []collect.MountStat{{Path: "/data", Dev: "/dev/sda1", Pct: 82}},
		Disks:  []collect.DiskStat{{Dev: "sda", ReadBps: 1 << 27}},
		Nets:   []collect.NetStat{{If: "eth0", RxBps: 40894464}},
		Zram:   []collect.ZramStat{{Dev: "zram0", Algo: "zstd", Ratio: 2.84}},
	}
}

func TestHealthz(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/healthz", nil))
	if rec.Code != 200 {
		t.Fatalf("healthz 状态码 %d", rec.Code)
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["ok"] != true {
		t.Fatalf("healthz 响应错误: %s", rec.Body.String())
	}
}

func TestNowBeforeFirstCollect(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/now", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("首次采集前应 503，实得 %d", rec.Code)
	}
}

func TestNowAndDimensionEndpoints(t *testing.T) {
	s, _ := newTestServer(t)
	s.Publish(sampleSnapshot())
	h := s.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/now", nil))
	if rec.Code != 200 {
		t.Fatalf("/now 状态码 %d", rec.Code)
	}
	var snap collect.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.CPU.Total != 42.6 || snap.CPU.Steal != 1.8 || snap.Host.Hostname != "prod-node-01" {
		t.Fatalf("/now 数据错误: cpu=%+v host=%+v", snap.CPU, snap.Host)
	}

	for _, tc := range []struct {
		path string
		want string
	}{
		{"/api/v1/mounts,", "/data"},
		{"/api/v1/disks", "sda"},
		{"/api/v1/nets", "eth0"},
		{"/api/v1/zram", "zram0"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", strings.TrimSuffix(tc.path, ","), nil))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("%s 应包含 %q，实得 %d %s", tc.path, tc.want, rec.Code, rec.Body.String())
		}
	}
}

func TestHistory(t *testing.T) {
	s, st := newTestServer(t)
	now := time.Now().Unix()
	st.InsertBatch(now-60, map[string]float64{"cpu.total": 40})
	st.InsertBatch(now-30, map[string]float64{"cpu.total": 42})

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/history?metric=cpu.total&range=1h", nil))
	if rec.Code != 200 {
		t.Fatalf("/history 状态码 %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Metric string         `json:"metric"`
		Layer  string         `json:"layer"`
		Points []store.Point  `json:"points"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Layer != "raw" || len(body.Points) != 2 {
		t.Fatalf("history 数据错误: %+v", body)
	}
}

func TestHistoryBadParams(t *testing.T) {
	s, _ := newTestServer(t)
	h := s.Handler()
	// 缺 metric
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/history", nil))
	if rec.Code != 400 {
		t.Fatal("缺 metric 应 400")
	}
	// 非法 range
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/history?metric=cpu.total&range=3y", nil))
	if rec.Code != 400 {
		t.Fatal("非法 range 应 400")
	}
}

func TestHistoryAutoLayer(t *testing.T) {
	if autoLayer(time.Hour) != "raw" {
		t.Fatal("1h 应选 raw")
	}
	if autoLayer(24*time.Hour) != "1m" {
		t.Fatal("24h 应选 1m")
	}
	if autoLayer(30*24*time.Hour) != "10m" {
		t.Fatal("30d 应选 10m")
	}
}

// TestSSEStream 通过真实 httptest 服务器验证 SSE 首帧与广播帧格式。
func TestSSEStream(t *testing.T) {
	s, _ := newTestServer(t)
	s.Publish(sampleSnapshot())
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type 错误: %q", ct)
	}
	reader := bufio.NewReader(resp.Body)
	// 首帧：连接即补当前快照
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "data: ") || !strings.Contains(line, "prod-node-01") {
		t.Fatalf("首帧格式错误: %q", line)
	}
	// 读掉首帧的尾部换行（SSE 每帧以 \n\n 结尾）
	sep, err := reader.ReadString('\n')
	if err != nil || sep != "\n" {
		t.Fatalf("首帧分隔符错误: %q %v", sep, err)
	}
	// 再广播一帧
	s.Publish(sampleSnapshot())
	line2, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line2, "data: ") {
		t.Fatalf("广播帧格式错误: %q", line2)
	}
}
