package v1

import "github.com/gogf/gf/v2/frame/g"

type HealthReq struct {
	g.Meta `path:"/healthz" method:"get" tags:"Health" summary:"探活"`
}

type HealthRes struct {
	Status string `json:"status"`
}
