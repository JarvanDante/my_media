package middleware

import (
	"net/http"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

// CORS 简易跨域
func CORS(r *ghttp.Request) {
	r.Response.CORSDefault()
	r.Middleware.Next()
}

// AdminToken 总后台调用鉴权(骨架: 比对配置 security.admin_token)
func AdminToken(r *ghttp.Request) {
	want := g.Cfg().MustGet(r.Context(), "security.admin_token").String()
	got := r.Header.Get("X-Admin-Token")
	if want == "" || got == "" || got != want {
		r.Response.WriteStatus(http.StatusUnauthorized)
		r.Response.WriteJsonExit(g.Map{"code": 401, "message": "invalid admin token", "data": nil})
		return
	}
	r.Middleware.Next()
}

// AppKey 子站开放 API 鉴权(骨架: 查 paas_client 表)
func AppKey(r *ghttp.Request) {
	key := r.Header.Get("X-App-Key")
	secret := r.Header.Get("X-App-Secret")
	if key == "" || secret == "" {
		r.Response.WriteStatus(http.StatusUnauthorized)
		r.Response.WriteJsonExit(g.Map{"code": 401, "message": "missing app credentials", "data": nil})
		return
	}
	one, err := g.DB().Model("paas_client").
		Where("app_key", key).
		Where("app_secret", secret).
		Where("status", 1).
		One()
	if err != nil || one.IsEmpty() {
		r.Response.WriteStatus(http.StatusUnauthorized)
		r.Response.WriteJsonExit(g.Map{"code": 401, "message": "invalid app credentials", "data": nil})
		return
	}
	r.SetCtxVar("app_key", key)
	r.SetCtxVar("site_code", one["site_code"].String())
	r.Middleware.Next()
}

// NotFound 404
func NotFound(r *ghttp.Request) {
	r.Response.WriteStatus(http.StatusNotFound)
	r.Response.WriteJson(g.Map{"code": 404, "message": "not found", "data": nil})
}
