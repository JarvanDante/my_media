package middleware

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

// 与 logic.DefaultDramaImportLimits 的公网可达默认保持一致（配置优先）。
const (
	defaultDramaMaxBodyBytes     int64 = 100_000_000
	defaultDramaImportTimeoutSec int   = 600
)

// ErrDramaBodyTooLarge 请求体累计读取超过上限（含未知长度、多字段/多文件）。
var ErrDramaBodyTooLarge = errors.New("request body too large")

type hardLimitBody struct {
	rc   io.ReadCloser
	max  int64
	read atomic.Int64
	hit  atomic.Bool
}

func newHardLimitBody(rc io.ReadCloser, max int64) *hardLimitBody {
	if rc == nil {
		rc = http.NoBody
	}
	return &hardLimitBody{rc: rc, max: max}
}

func (b *hardLimitBody) Read(p []byte) (int, error) {
	if b.hit.Load() {
		return 0, ErrDramaBodyTooLarge
	}
	if b.max < 0 {
		b.hit.Store(true)
		return 0, ErrDramaBodyTooLarge
	}
	got := b.read.Load()
	remain := b.max - got
	if remain <= 0 {
		b.hit.Store(true)
		return 0, ErrDramaBodyTooLarge
	}
	if int64(len(p)) > remain {
		p = p[:remain]
	}
	n, err := b.rc.Read(p)
	if n > 0 {
		total := b.read.Add(int64(n))
		if total >= b.max {
			// 恰好读满上限：本次数据仍交给解析器；再多读则拒绝。
			if total > b.max {
				b.hit.Store(true)
				return 0, ErrDramaBodyTooLarge
			}
		}
	}
	if err == nil && b.read.Load() >= b.max {
		// 若底层还能继续读，下一次 Read 会返回 ErrDramaBodyTooLarge
	}
	return n, err
}

func (b *hardLimitBody) Close() error {
	if b.rc != nil {
		return b.rc.Close()
	}
	return nil
}

func (b *hardLimitBody) exceeded() bool {
	return b.hit.Load()
}

func isDramaImportPath(r *ghttp.Request) bool {
	if r == nil || r.Request == nil {
		return false
	}
	p := strings.TrimRight(r.URL.Path, "/")
	method := r.Method
	if method == http.MethodPost && (p == "/admin/dramas/import" || strings.HasSuffix(p, "/admin/dramas/import")) {
		return true
	}
	// PUT /admin/dramas/uploads/{id}/parts/{n} 代传分片
	if method == http.MethodPut || method == http.MethodPost {
		if strings.Contains(p, "/admin/dramas/uploads/") && strings.Contains(p, "/parts/") {
			return true
		}
	}
	return false
}

func dramaImportHTTPLimits(ctx context.Context) (maxBody int64, timeoutSec int) {
	maxBody = defaultDramaMaxBodyBytes
	timeoutSec = defaultDramaImportTimeoutSec
	if ctx == nil {
		ctx = context.Background()
	}
	if n := g.Cfg().MustGet(ctx, "drama_import.max_body_bytes", 0).Int64(); n > 0 {
		maxBody = n
	}
	if n := g.Cfg().MustGet(ctx, "drama_import.import_timeout_sec", 0).Int(); n > 0 {
		timeoutSec = n
	}
	return maxBody, timeoutSec
}

func writeDramaBodyTooLarge(r *ghttp.Request, maxBody int64) {
	r.Response.ClearBuffer()
	r.Response.Status = http.StatusRequestEntityTooLarge
	r.SetError(ErrDramaBodyTooLarge)
	r.Response.WriteJson(g.Map{
		"code":    413,
		"message": "请求体超过上限 " + humanLimitBytes(maxBody) + "（含表单全部字段与文件）",
		"data":    nil,
	})
}

func humanLimitBytes(n int64) string {
	if n >= 1024*1024*1024 {
		return fmt.Sprintf("%.2fGB", float64(n)/1024/1024/1024)
	}
	if n >= 1024*1024 {
		return fmt.Sprintf("%.1fMB", float64(n)/1024/1024)
	}
	if n >= 1024 {
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	}
	return fmt.Sprintf("%dB", n)
}

// LimitDramaImportBody 在路由绑定和 multipart 解析之前限制短剧导入请求体，
// 并为该请求设置 ImportTimeoutSec 的上下文截止时间。
// 已知 Content-Length 超限立即 413；未知长度按累计读取硬限；多文件/额外字段一并计入。
func LimitDramaImportBody(r *ghttp.Request) {
	LimitDramaImportBodyWith(r, 0, 0)
}

// LimitDramaImportBodyWith 供测试注入上限；maxBody/timeoutSec 为 0 时读配置或默认值。
func LimitDramaImportBodyWith(r *ghttp.Request, maxBody int64, timeoutSec int) {
	if !isDramaImportPath(r) {
		r.Middleware.Next()
		return
	}
	cfgBody, cfgTimeout := dramaImportHTTPLimits(r.Context())
	if maxBody <= 0 {
		maxBody = cfgBody
	}
	if timeoutSec <= 0 {
		timeoutSec = cfgTimeout
	}
	if timeoutSec > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeoutSec)*time.Second)
		defer cancel()
		r.SetCtx(ctx)
	}
	if r.Request != nil {
		cl := r.Request.ContentLength
		if cl > maxBody {
			writeDramaBodyTooLarge(r, maxBody)
			return
		}
		lim := newHardLimitBody(r.Request.Body, maxBody)
		r.Request.Body = lim
		r.Middleware.Next()
		if lim.exceeded() || errorsIsBodyTooLarge(r.GetError()) {
			writeDramaBodyTooLarge(r, maxBody)
		}
		return
	}
	r.Middleware.Next()
}

func errorsIsBodyTooLarge(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrDramaBodyTooLarge) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "request body too large") || strings.Contains(msg, "http: request body too large")
}
