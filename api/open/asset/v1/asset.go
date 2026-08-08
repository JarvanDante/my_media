package v1

import "github.com/gogf/gf/v2/frame/g"

type AssetItem struct {
	Id          int64  `json:"id"`
	Title       string `json:"title"`
	CoverUrl    string `json:"cover_url"`
	PlayUrl     string `json:"play_url"`
	DurationSec int    `json:"duration_sec"`
	Picked      bool   `json:"picked"` // 当前站点是否已选用
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
	AssetId     int64  `json:"asset_id"`
	Title       string `json:"title"`
	CoverUrl    string `json:"cover_url"`
	PlayUrl     string `json:"play_url"`
	PlayKey     string `json:"play_key"`
	DurationSec int    `json:"duration_sec"`
}

type PickListReq struct {
	g.Meta `path:"/open/picks" method:"get" tags:"Open/Asset" summary:"本站已选用列表"`
	Page   int `json:"page" d:"1"`
	Size   int `json:"size" d:"20"`
}

type PickListItem struct {
	AssetId     int64  `json:"asset_id"`
	Title       string `json:"title"`
	CoverUrl    string `json:"cover_url"`
	PlayUrl     string `json:"play_url"`
	PlayKey     string `json:"play_key"`
	DurationSec int    `json:"duration_sec"`
	PickedAt    string `json:"picked_at"`
}

type PickListRes struct {
	List  []PickListItem `json:"list"`
	Total int            `json:"total"`
}
