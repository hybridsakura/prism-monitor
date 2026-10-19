package api

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hybridsakura/prism-monitor/internal/collect"
	"github.com/hybridsakura/prism-monitor/internal/store"
	"github.com/hybridsakura/prism-monitor/web"
)

func TestSPAEmbedding(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	c := collect.New("..")
	srv := New(c, st, web.FS)
	h := srv.Handler()

	// 1. 访问 / 应返回 index.html
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Prism Monitor") {
		t.Fatalf("根路径应返回 SPA HTML，实得 %d %s", rec.Code, rec.Body.String())
	}

	// 2. 访问 /style.css 应返回样式表
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/style.css", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "--bg-app") {
		t.Fatalf("style.css 错误: %d", rec.Code)
	}

	// 3. 访问 /vendor/echarts.min.js
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/vendor/echarts.min.js", nil))
	if rec.Code != 200 || rec.Body.Len() < 100000 {
		t.Fatalf("echarts.min.js 应为大文件，实得 %d bytes=%d", rec.Code, rec.Body.Len())
	}

	// 4. 前端路由回退：访问 /history 应回退到 index.html
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/history", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Prism Monitor") {
		t.Fatalf("未知路由应回退 SPA，实得 %d", rec.Code)
	}
}
