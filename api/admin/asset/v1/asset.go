package v1

import "github.com/gogf/gf/v2/frame/g"

type AssetItem struct {
	Id              string `json:"id"` // 对外 8 位短码
	Title           string `json:"title"`
	CoverUrl        string `json:"cover_url"`
	Status          int    `json:"status"`
	TranscodeStatus string `json:"transcode_status"`
	PlayUrl         string `json:"play_url"`
	DurationSec     int    `json:"duration_sec"`
	CreatedAt       string `json:"created_at"`
}

type ListReq struct {
	g.Meta  `path:"/admin/assets" method:"get" tags:"Admin/Asset" summary:"媒资池列表"`
	Page    int    `json:"page" d:"1"`
	Size    int    `json:"size" d:"20"`
	Keyword string `json:"keyword"`
	Status  int    `json:"status" d:"-1"`
}

type ListRes struct {
	List  []AssetItem `json:"list"`
	Total int         `json:"total"`
}

type CreateReq struct {
	g.Meta   `path:"/admin/assets" method:"post" tags:"Admin/Asset" summary:"创建资产元数据"`
	Title    string `json:"title" v:"required#标题必填"`
	CoverUrl string `json:"cover_url"`
	Remark   string `json:"remark"`
}

type CreateRes struct {
	Id string `json:"id"` // 8 位短码
}

type DetailReq struct {
	g.Meta `path:"/admin/assets/{id}" method:"get" tags:"Admin/Asset" summary:"资产详情"`
	Id     string `json:"id" in:"path" v:"required|length:8,8#资产id为8位"`
}

type DetailRes struct {
	AssetItem
	SourceBucket   string `json:"source_bucket"`
	SourceKey      string `json:"source_key"`
	PlayKey        string `json:"play_key"`
	TranscodeJobId string `json:"transcode_job_id"`
	TranscodeError string `json:"transcode_error"`
	Remark         string `json:"remark"`
}

type UploadURLReq struct {
	g.Meta   `path:"/admin/assets/{id}/upload-url" method:"post" tags:"Admin/Asset" summary:"预签名上传(M1)"`
	Id       string `json:"id" in:"path" v:"required|length:8,8"`
	Filename string `json:"filename" d:"video.mp4"`
}

type UploadURLRes struct {
	UploadUrl string `json:"upload_url"`
	Method    string `json:"method"`
	Bucket    string `json:"bucket"`
	Key       string `json:"key"`
	ExpireSec int    `json:"expire_sec"`
}

type TranscodeReq struct {
	g.Meta       `path:"/admin/assets/{id}/transcode" method:"post" tags:"Admin/Asset" summary:"触发转码(M1)"`
	Id           string `json:"id" in:"path" v:"required|length:8,8"`
	CoverSeekSec int    `json:"cover_seek_sec" d:"8" v:"min:0|max:36000#封面截取秒数无效"`
}

type TranscodeRes struct {
	JobId string `json:"job_id"`
}

type DeleteReq struct {
	g.Meta `path:"/admin/assets/{id}" method:"delete" tags:"Admin/Asset" summary:"删除资产并清理对象存储"`
	Id     string `json:"id" in:"path" v:"required|length:8,8"`
}

type DeleteRes struct {
	DeletedObjects int `json:"deleted_objects"`
}
