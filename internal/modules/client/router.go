package client

import (
	"github.com/gogf/gf/v2/net/ghttp"

	"github.com/JarvanDante/my_media/internal/dao"
	"github.com/JarvanDante/my_media/internal/modules/client/controller"
)

func RegisterAdmin(group *ghttp.RouterGroup, repo *dao.ClientRepo) {
	ctrl := controller.NewAdmin(repo)
	group.Bind(ctrl.List, ctrl.Upsert, ctrl.Disable)
}
