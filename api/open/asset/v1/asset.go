package v1

import "github.com/gogf/gf/v2/frame/g"

type AssetItem struct {
	Id          int64  `json:"id"`
	Title       string `json:"title"`
	CoverUrl    string `json:"cover_url"`
	PlayUrl     string `json:"play_url"`
	DurationSec int    `json:"duration_sec"`
}

type ListReq struct {
	g.Meta  `path:"/open/assets" method:"get" tags:"Open/Asset" summary:"可选用媒资列表"`
	Page    int    `json:"page" d:"1"`
	Size    int    `json:"size" d:"20"`
	Keyword string `json:"keyword"`
}

type ListRes struct {
	List  []AssetItem `json:"list"`
	Total int         `json:"total"`
}

type DetailReq struct {
	g.Meta `path:"/open/assets/{id}" method:"get" tags:"Open/Asset" summary:"媒资详情"`
	Id     int64 `json:"id" in:"path" v:"required|min:1"`
}

type DetailRes struct {
	AssetItem
	PlayKey string `json:"play_key"`
}

type PickReq struct {
	g.Meta `path:"/open/assets/{id}/pick" method:"post" tags:"Open/Asset" summary:"选用媒资"`
	Id     int64 `json:"id" in:"path" v:"required|min:1"`
}

type PickRes struct {
	AssetId int64  `json:"asset_id"`
	PlayUrl string `json:"play_url"`
}
