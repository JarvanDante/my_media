package v1

import "github.com/gogf/gf/v2/frame/g"

type AssetItem struct {
	Id              int64  `json:"id"`
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
	Status  int    `json:"status" d:"-1"` // -1全部
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
	Id int64 `json:"id"`
}

type DetailReq struct {
	g.Meta `path:"/admin/assets/{id}" method:"get" tags:"Admin/Asset" summary:"资产详情"`
	Id     int64 `json:"id" in:"path" v:"required|min:1"`
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
	g.Meta `path:"/admin/assets/{id}/upload-url" method:"post" tags:"Admin/Asset" summary:"预签名上传(M1)"`
	Id     int64 `json:"id" in:"path" v:"required|min:1"`
}

type UploadURLRes struct {
	UploadUrl string `json:"upload_url"`
	Bucket    string `json:"bucket"`
	Key       string `json:"key"`
}

type TranscodeReq struct {
	g.Meta `path:"/admin/assets/{id}/transcode" method:"post" tags:"Admin/Asset" summary:"触发转码(M1)"`
	Id     int64 `json:"id" in:"path" v:"required|min:1"`
}

type TranscodeRes struct {
	JobId string `json:"job_id"`
}
