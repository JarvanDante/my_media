package v1

import "github.com/gogf/gf/v2/frame/g"

type ComicPageItem struct {
	Filename string `json:"filename"`
	Key      string `json:"key"`
	Url      string `json:"url"`
}

type ComicChapterItem struct {
	Seq       int             `json:"seq"`
	Title     string          `json:"title"`
	PageCount int             `json:"page_count"`
	Pages     []ComicPageItem `json:"pages"`
}

type AssetItem struct {
	Id           string `json:"id"` // 对外短码(新 16 位，历史可能 8 位)
	Title        string `json:"title"`
	CoverUrl     string `json:"cover_url"`
	PlayUrl      string `json:"play_url"`
	DurationSec  int    `json:"duration_sec"`
	Kind         int    `json:"kind"`
	Intro        string `json:"intro,omitempty"`
	ChapterCount int    `json:"chapter_count,omitempty"`
	Picked       bool   `json:"picked"`
}

type ListReq struct {
	g.Meta  `path:"/open/assets" method:"get" tags:"Open/Asset" summary:"可选用媒资列表"`
	Page    int    `json:"page" d:"1"`
	Size    int    `json:"size" d:"20"`
	Keyword string `json:"keyword"`
	Kind    int    `json:"kind" d:"0"` // 0视频 1漫画 2动漫；默认视频以免旧站点拉到其它类型
}

type ListRes struct {
	List  []AssetItem `json:"list"`
	Total int         `json:"total"`
}

type DetailReq struct {
	g.Meta `path:"/open/assets/{id}" method:"get" tags:"Open/Asset" summary:"媒资详情"`
	Id     string `json:"id" in:"path" v:"required|length:8,16"`
}

type DetailRes struct {
	AssetItem
	PlayKey  string             `json:"play_key"`
	Intro    string             `json:"intro,omitempty"`
	Chapters []ComicChapterItem `json:"chapters,omitempty"`
}

type PickReq struct {
	g.Meta `path:"/open/assets/{id}/pick" method:"post" tags:"Open/Asset" summary:"选用媒资"`
	Id     string `json:"id" in:"path" v:"required|length:8,16"`
}

type PickRes struct {
	Id           string             `json:"id"`
	Title        string             `json:"title"`
	CoverUrl     string             `json:"cover_url"`
	PlayUrl      string             `json:"play_url"`
	PlayKey      string             `json:"play_key"`
	DurationSec  int                `json:"duration_sec"`
	Kind         int                `json:"kind"`
	Intro        string             `json:"intro,omitempty"`
	ChapterCount int                `json:"chapter_count,omitempty"`
	Chapters     []ComicChapterItem `json:"chapters,omitempty"`
}

type PickListReq struct {
	g.Meta `path:"/open/picks" method:"get" tags:"Open/Asset" summary:"本站已选用列表"`
	Page   int `json:"page" d:"1"`
	Size   int `json:"size" d:"20"`
}

type PickListItem struct {
	Id          string `json:"id"`
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

// PlayTokenReq 签发播放地址(需已选用): 可选试看秒数与绑定IP。
type PlayTokenReq struct {
	g.Meta     `path:"/open/assets/{id}/play-token" method:"post" tags:"Open/Asset" summary:"签发播放地址(可试看/绑IP)"`
	Id         string `json:"id" in:"path" v:"required|length:8,16"`
	PreviewSec int    `json:"preview_sec" v:"min:0"`
	ClientIp   string `json:"client_ip"`
}

type PlayTokenRes struct {
	PlayUrl   string `json:"play_url"`
	ExpiresAt int64  `json:"expires_at"`
}
