package asset

import (
	"github.com/gogf/gf/v2/net/ghttp"

	"github.com/JarvanDante/my_media/internal/modules/asset/controller"
	"github.com/JarvanDante/my_media/internal/modules/asset/service"
	openc "github.com/JarvanDante/my_media/internal/modules/open/controller"
)

func RegisterAdmin(group *ghttp.RouterGroup, svc service.Asset) {
	ctrl := controller.NewAdmin(svc)
	group.Bind(ctrl.List, ctrl.Create, ctrl.Detail, ctrl.UploadURL, ctrl.Transcode)
}

func RegisterOpen(group *ghttp.RouterGroup, svc service.Asset) {
	ctrl := openc.NewOpen(svc)
	group.Bind(ctrl.List, ctrl.Detail, ctrl.Pick, ctrl.PickList)
}
