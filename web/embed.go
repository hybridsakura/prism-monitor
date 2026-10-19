package web

import "embed"

// FS 内嵌前端静态资源（index.html、app.js、style.css、vendor/echarts.min.js 等）。
//
//go:embed *
var FS embed.FS
