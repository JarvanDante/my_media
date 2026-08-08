package health

import (
	"context"

	"github.com/gogf/gf/v2/net/ghttp"

	v1 "github.com/JarvanDante/my_media/api/health/v1"
)

type Controller struct{}

func Register(group *ghttp.RouterGroup) {
	group.Bind(&Controller{})
}

func (c *Controller) Health(ctx context.Context, req *v1.HealthReq) (res *v1.HealthRes, err error) {
	return &v1.HealthRes{Status: "ok"}, nil
}
