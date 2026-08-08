package asset

import (
	"github.com/gogf/gf/v2/net/ghttp"

	"github.com/JarvanDante/my_media/internal/modules/asset/controller"
	"github.com/JarvanDante/my_media/internal/modules/asset/domain"
	"github.com/JarvanDante/my_media/internal/modules/asset/logic"
	openc "github.com/JarvanDante/my_media/internal/modules/open/controller"
)

func RegisterAdmin(group *ghttp.RouterGroup, repo domain.Repository) {
	ctrl := controller.NewAdmin(logic.New(repo))
	group.Bind(ctrl.List, ctrl.Create, ctrl.Detail, ctrl.UploadURL, ctrl.Transcode)
}

func RegisterOpen(group *ghttp.RouterGroup, repo domain.Repository) {
	ctrl := openc.NewOpen(logic.New(repo))
	group.Bind(ctrl.List, ctrl.Detail, ctrl.Pick)
}
