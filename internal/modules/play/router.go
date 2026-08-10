package play

import (
	"github.com/gogf/gf/v2/net/ghttp"

	"github.com/JarvanDante/my_media/internal/dao"
)

// RegisterAdmin 管理端(X-Admin-Token 分组)。
func RegisterAdmin(group *ghttp.RouterGroup, repo *dao.PlayRepo) {
	ctrl := New(repo)
	group.Bind(ctrl.PolicyList, ctrl.PolicyUpsert, ctrl.Stats)
}

// RegisterGw 网关内部(X-Play-Token 分组)。
func RegisterGw(group *ghttp.RouterGroup, repo *dao.PlayRepo) {
	ctrl := New(repo)
	group.Bind(ctrl.GwPolicies, ctrl.GwStatsIngest)
}
