package v1

import "github.com/gogf/gf/v2/frame/g"

type ClientItem struct {
	Id        int64  `json:"id"`
	AppKey    string `json:"app_key"`
	SiteCode  string `json:"site_code"`
	Status    int    `json:"status"`
	Remark    string `json:"remark"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type ListReq struct {
	g.Meta  `path:"/admin/clients" method:"get" tags:"Admin/Client" summary:"PaaS 调用方列表"`
	Page    int    `json:"page" d:"1"`
	Size    int    `json:"size" d:"20"`
	Keyword string `json:"keyword"`
}

type ListRes struct {
	List  []ClientItem `json:"list"`
	Total int          `json:"total"`
}

// UpsertReq 总后台同步站点凭证(明文 secret 只在此写入一次, 落库哈希)
type UpsertReq struct {
	g.Meta    `path:"/admin/clients" method:"put" tags:"Admin/Client" summary:"同步/登记调用方凭证"`
	AppKey    string `json:"app_key" v:"required#app_key必填"`
	AppSecret string `json:"app_secret" v:"required#app_secret必填"`
	SiteCode  string `json:"site_code"`
	Status    int    `json:"status" d:"1"` // 1启用 0停用
	Remark    string `json:"remark"`
}

type UpsertRes struct {
	AppKey   string `json:"app_key"`
	SiteCode string `json:"site_code"`
	Status   int    `json:"status"`
}

type DisableReq struct {
	g.Meta `path:"/admin/clients/{app_key}/disable" method:"post" tags:"Admin/Client" summary:"停用调用方"`
	AppKey string `json:"app_key" in:"path" v:"required"`
}

type DisableRes struct {
	AppKey string `json:"app_key"`
	Status int    `json:"status"`
}
