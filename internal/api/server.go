// Package api 提供 prism-monitor 的 HTTP 接口：REST + SSE 实时推送 + 内嵌 SPA。
package api

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hybridsakura/prism-monitor/internal/collect"
	"github.com/hybridsakura/prism-monitor/internal/store"
)

// Server 聚合采集器、存储与 SSE 广播。
type Server struct {
	coll *collect.Collector
	st   *store.Store
	hub  *sseHub
	web  fs.FS

	latest atomic.Value // *collect.Snapshot
}

// New 装配路由。webFS 为内嵌前端静态资源（可为 nil，仅 API 模式）。
func New(c *collect.Collector, st *store.Store, webFS fs.FS) *Server {
	s := &Server{coll: c, st: st, hub: newSSEHub(), web: webFS}
	return s
}

// Publish 供采集循环调用：更新最新快照并广播给所有 SSE 客户端。
func (s *Server) Publish(snap *collect.Snapshot) {
	s.latest.Store(snap)
	if data, err := json.Marshal(snap); err == nil {
		s.hub.broadcast(data)
	}
}

// Handler 返回根路由。
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Get("/api/v1/healthz", s.handleHealthz)
	r.Get("/api/v1/now", s.handleNow)
	r.Get("/api/v1/stream", s.handleStream)
	r.Get("/api/v1/history", s.handleHistory)
	r.Get("/api/v1/mounts", s.handleMounts)
	r.Get("/api/v1/disks", s.handleDisks)
	r.Get("/api/v1/nets", s.handleNets)
	r.Get("/api/v1/zram", s.handleZram)
	if s.web != nil {
		r.Get("/*", s.spaHandler())
	}
	return r
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ts": time.Now().Unix()})
}

func (s *Server) current() *collect.Snapshot {
	if v := s.latest.Load(); v != nil {
		return v.(*collect.Snapshot)
	}
	return nil
}

func (s *Server) handleNow(w http.ResponseWriter, _ *http.Request) {
	snap := s.current()
	if snap == nil {
		writeErr(w, http.StatusServiceUnavailable, "首次采集中，请稍候")
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleMounts(w http.ResponseWriter, _ *http.Request) {
	if snap := s.current(); snap != nil {
		writeJSON(w, http.StatusOK, snap.Mounts)
		return
	}
	writeJSON(w, http.StatusOK, []collect.MountStat{})
}

func (s *Server) handleDisks(w http.ResponseWriter, _ *http.Request) {
	if snap := s.current(); snap != nil {
		writeJSON(w, http.StatusOK, snap.Disks)
		return
	}
	writeJSON(w, http.StatusOK, []collect.DiskStat{})
}

func (s *Server) handleNets(w http.ResponseWriter, _ *http.Request) {
	if snap := s.current(); snap != nil {
		writeJSON(w, http.StatusOK, snap.Nets)
		return
	}
	writeJSON(w, http.StatusOK, []collect.NetStat{})
}

func (s *Server) handleZram(w http.ResponseWriter, _ *http.Request) {
	if snap := s.current(); snap != nil {
		writeJSON(w, http.StatusOK, snap.Zram)
		return
	}
	writeJSON(w, http.StatusOK, []collect.ZramStat{})
}

// rangeToDuration 支持的时间范围。
var rangeToDuration = map[string]time.Duration{
	"15m": 15 * time.Minute,
	"1h":  time.Hour,
	"6h":  6 * time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
	"90d": 90 * 24 * time.Hour,
}

// autoLayer 按范围选择降采样层。
func autoLayer(d time.Duration) string {
	switch {
	case d <= 2*time.Hour:
		return "raw"
	case d <= 7*24*time.Hour:
		return "1m"
	default:
		return "10m"
	}
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	metric := r.URL.Query().Get("metric")
	if metric == "" {
		writeErr(w, http.StatusBadRequest, "缺少 metric 参数")
		return
	}
	rangeStr := r.URL.Query().Get("range")
	if rangeStr == "" {
		rangeStr = "1h"
	}
	d, ok := rangeToDuration[rangeStr]
	if !ok {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("不支持的 range: %q（可选 15m/1h/6h/24h/7d/30d/90d）", rangeStr))
		return
	}
	layer := r.URL.Query().Get("layer")
	if layer == "" || layer == "auto" {
		layer = autoLayer(d)
	}
	to := time.Now().Unix()
	from := to - int64(d.Seconds())
	pts, err := s.st.Query(metric, from, to, layer)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"metric": metric,
		"range":  rangeStr,
		"layer":  layer,
		"points": pts,
	})
}

// spaHandler 静态资源 + SPA 回退到 index.html。
func (s *Server) spaHandler() http.HandlerFunc {
	fileServer := http.FileServerFS(s.web)
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}
		// 存在的静态文件直接服务，否则回退 index.html（前端路由）
		if f, err := s.web.Open(path[1:]); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	}
}

// parseInt 辅助（保留给后续分页参数）。
func parseInt(s string, def int64) int64 {
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v
	}
	return def
}
