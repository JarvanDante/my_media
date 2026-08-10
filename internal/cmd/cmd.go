// Package cmd 启动装配。
package cmd

import (
	"context"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gcmd"

	"github.com/JarvanDante/my_media/internal/dao"
	"github.com/JarvanDante/my_media/internal/modules/asset"
	"github.com/JarvanDante/my_media/internal/modules/asset/logic"
	"github.com/JarvanDante/my_media/internal/modules/client"
	"github.com/JarvanDante/my_media/internal/modules/health"
	playmod "github.com/JarvanDante/my_media/internal/modules/play"
	"github.com/JarvanDante/my_media/internal/shared/middleware"
	"github.com/JarvanDante/my_media/internal/shared/mq"
	"github.com/JarvanDante/my_media/internal/shared/storage"
)

// Main 媒资中心 API(:8004)。
var Main = gcmd.Command{
	Name:  "mediaapi",
	Brief: "媒资中心(PaaS) API",
	Func: func(ctx context.Context, parser *gcmd.Parser) error {
		store, err := storage.NewMinio(ctx)
		if err != nil {
			g.Log().Warningf(ctx, "minio init failed: %v (upload/transcode 将不可用)", err)
		}
		bus := mq.NewBus(ctx)
		repo := dao.NewAssetRepo()
		svc := logic.New(repo, store, bus)

		// 后台消费转码结果
		go func() {
			bg := context.Background()
			if err := bus.ConsumeResults(bg, svc.HandleTranscodeResult); err != nil {
				g.Log().Errorf(bg, "kafka result consumer stopped: %v", err)
			}
		}()

		s := g.Server()
		s.Use(middleware.CORS, ghttp.MiddlewareHandlerResponse)
		s.BindStatusHandler(404, middleware.NotFound)

		health.Register(s.Group("/"))

		clientRepo := dao.NewClientRepo()
		playRepo := dao.NewPlayRepo()
		s.Group("/", func(group *ghttp.RouterGroup) {
			group.Middleware(middleware.AdminToken)
			asset.RegisterAdmin(group, svc)
			client.RegisterAdmin(group, clientRepo)
			playmod.RegisterAdmin(group, playRepo)
		})

		s.Group("/", func(group *ghttp.RouterGroup) {
			group.Middleware(middleware.PlayToken)
			playmod.RegisterGw(group, playRepo)
		})

		s.Group("/", func(group *ghttp.RouterGroup) {
			group.Middleware(middleware.AppKey)
			asset.RegisterOpen(group, svc)
		})

		s.Run()
		_ = bus.Close()
		return nil
	},
}
