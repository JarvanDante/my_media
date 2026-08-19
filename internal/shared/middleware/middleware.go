package middleware

import (
	"net/http"
	"strings"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"

	"github.com/JarvanDante/my_media/internal/dao"
	"github.com/JarvanDante/my_media/internal/shared/authz"
)

// CORS 简易跨域
func CORS(r *ghttp.Request) {
	r.Response.CORSDefault()
	r.Middleware.Next()
}

// JSONErrors 把 multipart 体过大等框架原始错误转成 JSON，避免前端 JSON.parse 失败。
func JSONErrors(r *ghttp.Request) {
	r.Middleware.Next()
	body := strings.TrimSpace(r.Response.BufferString())
	dump := body
	if err := r.GetError(); err != nil {
		dump += " " + err.Error()
	}
	if !strings.Contains(dump, "ParseMultipartForm") && !strings.Contains(dump, "request body too large") {
		return
	}
	r.Response.ClearBuffer()
	r.Response.Status = http.StatusRequestEntityTooLarge
	r.Response.WriteJson(g.Map{
		"code":    413,
		"message": "压缩包太大，单包不能超过 2GB",
		"data":    nil,
	})
}

// AdminToken 总后台调用鉴权
func AdminToken(r *ghttp.Request) {
	want := g.Cfg().MustGet(r.Context(), "security.admin_token").String()
	got := r.Header.Get("X-Admin-Token")
	if want == "" || got == "" || got != want {
		r.Response.Status = http.StatusUnauthorized
		r.Response.WriteJsonExit(g.Map{"code": 401, "message": "invalid admin token", "data": nil})
		return
	}
	r.Middleware.Next()
}

// AppKey 子站开放 API：查 paas_client，密钥哈希/明文兼容 + 恒定时间比较
func AppKey(r *ghttp.Request) {
	key := r.Header.Get("X-App-Key")
	secret := r.Header.Get("X-App-Secret")
	if key == "" || secret == "" {
		r.Response.Status = http.StatusUnauthorized
		r.Response.WriteJsonExit(g.Map{"code": 401, "message": "missing app credentials", "data": nil})
		return
	}
	cli, err := dao.NewClientRepo().FindActive(r.Context(), key)
	if err != nil || cli == nil || !authz.MatchSecret(secret, cli.AppSecret, cli.SecretHashed == 1) {
		r.Response.Status = http.StatusUnauthorized
		r.Response.WriteJsonExit(g.Map{"code": 401, "message": "invalid app credentials", "data": nil})
		return
	}
	// 明文旧数据命中后静默升级为哈希
	if cli.SecretHashed == 0 {
		_ = dao.NewClientRepo().Upsert(r.Context(), key, secret, cli.SiteCode, cli.Remark, 1)
	}
	r.SetCtxVar("app_key", key)
	r.SetCtxVar("site_code", cli.SiteCode)
	r.Middleware.Next()
}

// NotFound 404
func NotFound(r *ghttp.Request) {
	r.Response.WriteStatus(http.StatusNotFound)
	r.Response.WriteJson(g.Map{"code": 404, "message": "not found", "data": nil})
}

// PlayToken 网关内部接口鉴权: X-Play-Token 必须等于 play_gateway.secret。
func PlayToken(r *ghttp.Request) {
	want := g.Cfg().MustGet(r.Context(), "play_gateway.secret").String()
	got := r.Header.Get("X-Play-Token")
	if want == "" || got != want {
		r.Response.Status = http.StatusUnauthorized
		r.Response.WriteJsonExit(g.Map{"code": 401, "message": "invalid play token", "data": nil})
		return
	}
	r.Middleware.Next()
}
