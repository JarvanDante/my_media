// Package cmd 启动装配。
package cmd

import (
	"context"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gcmd"

	"github.com/JarvanDante/my_media/internal/dao"
	"github.com/JarvanDante/my_media/internal/modules/asset"
	"github.com/JarvanDante/my_media/internal/modules/health"
	"github.com/JarvanDante/my_media/internal/shared/middleware"
)

// Main 媒资中心 API(:8004)。
var Main = gcmd.Command{
	Name:  "mediaapi",
	Brief: "媒资中心(PaaS) API",
	Func: func(ctx context.Context, parser *gcmd.Parser) error {
		s := g.Server()
		s.Use(middleware.CORS, ghttp.MiddlewareHandlerResponse)
		s.BindStatusHandler(404, middleware.NotFound)

		repo := dao.NewAssetRepo()

		// 探活(无鉴权)
		health.Register(s.Group("/"))

		// 总后台
		s.Group("/", func(group *ghttp.RouterGroup) {
			group.Middleware(middleware.AdminToken)
			asset.RegisterAdmin(group, repo)
		})

		// 子站开放
		s.Group("/", func(group *ghttp.RouterGroup) {
			group.Middleware(middleware.AppKey)
			asset.RegisterOpen(group, repo)
		})

		s.Run()
		return nil
	},
}
