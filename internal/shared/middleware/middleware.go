package middleware

import (
	"net/http"

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

// AdminToken 总后台调用鉴权
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

// AppKey 子站开放 API：查 paas_client，密钥哈希/明文兼容 + 恒定时间比较
func AppKey(r *ghttp.Request) {
	key := r.Header.Get("X-App-Key")
	secret := r.Header.Get("X-App-Secret")
	if key == "" || secret == "" {
		r.Response.WriteStatus(http.StatusUnauthorized)
		r.Response.WriteJsonExit(g.Map{"code": 401, "message": "missing app credentials", "data": nil})
		return
	}
	cli, err := dao.NewClientRepo().FindActive(r.Context(), key)
	if err != nil || cli == nil || !authz.MatchSecret(secret, cli.AppSecret, cli.SecretHashed == 1) {
		r.Response.WriteStatus(http.StatusUnauthorized)
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
