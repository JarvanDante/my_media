package asset

import (
	"github.com/gogf/gf/v2/net/ghttp"

	"github.com/JarvanDante/my_media/internal/modules/asset/controller"
	"github.com/JarvanDante/my_media/internal/modules/asset/service"
	openc "github.com/JarvanDante/my_media/internal/modules/open/controller"
)

func RegisterAdmin(group *ghttp.RouterGroup, svc service.Asset) {
	ctrl := controller.NewAdmin(svc)
	group.Bind(ctrl.List, ctrl.Create, ctrl.UpdateAsset, ctrl.Detail, ctrl.UploadURL, ctrl.Transcode, ctrl.ReplaceCover, ctrl.Delete, ctrl.DeleteChapter, ctrl.CreateNovelChapter, ctrl.UpdateNovelChapter, ctrl.CreateEpisode, ctrl.ImportComics, ctrl.ImportNovels, ctrl.ImportDramas, ctrl.DramaImportLimits)
}

func RegisterOpen(group *ghttp.RouterGroup, svc service.Asset) {
	ctrl := openc.NewOpen(svc)
	group.Bind(ctrl.List, ctrl.Detail, ctrl.Pick, ctrl.PickList, ctrl.PlayToken)
}
