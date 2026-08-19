package v1

import "github.com/gogf/gf/v2/frame/g"

type AssetItem struct {
	Id              string `json:"id"` // 对外短码(新 16 位，历史可能 8 位)
	Title           string `json:"title"`
	CoverUrl        string `json:"cover_url"`
	Status          int    `json:"status"`
	TranscodeStatus string `json:"transcode_status"`
	PlayUrl         string `json:"play_url"`
	DurationSec     int    `json:"duration_sec"`
	Kind            int    `json:"kind"`
	Category        string `json:"category"`
	ChapterCount    int    `json:"chapter_count"`
	CreatedAt       string `json:"created_at"`
}

type ListReq struct {
	g.Meta  `path:"/admin/assets" method:"get" tags:"Admin/Asset" summary:"媒资池列表"`
	Page    int    `json:"page" d:"1"`
	Size    int    `json:"size" d:"20"`
	Keyword string `json:"keyword"`
	Status  int    `json:"status" d:"-1"`
	Kind    int    `json:"kind" d:"-1"` // -1全部 0视频 1漫画
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
	Id string `json:"id"` // 对外短码
}

type DetailReq struct {
	g.Meta `path:"/admin/assets/{id}" method:"get" tags:"Admin/Asset" summary:"资产详情"`
	Id     string `json:"id" in:"path" v:"required|length:8,16#资产id为8~16位"`
}

type DetailRes struct {
	AssetItem
	SourceBucket   string `json:"source_bucket"`
	SourceKey      string `json:"source_key"`
	PlayKey        string `json:"play_key"`
	TranscodeJobId string `json:"transcode_job_id"`
	TranscodeError string `json:"transcode_error"`
	Remark         string `json:"remark"`
	Intro          string `json:"intro"`
	Chapters       []ComicChapterItem `json:"chapters,omitempty"`
}

type UploadURLReq struct {
	g.Meta   `path:"/admin/assets/{id}/upload-url" method:"post" tags:"Admin/Asset" summary:"预签名上传(M1)"`
	Id       string `json:"id" in:"path" v:"required|length:8,16"`
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
	Id           string `json:"id" in:"path" v:"required|length:8,16"`
	CoverSeekSec int    `json:"cover_seek_sec" d:"8" v:"min:0|max:36000#封面截取秒数无效"`
}

type TranscodeRes struct {
	JobId string `json:"job_id"`
}

type DeleteReq struct {
	g.Meta `path:"/admin/assets/{id}" method:"delete" tags:"Admin/Asset" summary:"删除资产并清理对象存储"`
	Id     string `json:"id" in:"path" v:"required|length:8,16"`
}

type DeleteRes struct {
	DeletedObjects int `json:"deleted_objects"`
}

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

type ImportComicsReq struct {
	g.Meta `path:"/admin/comics/import" method:"post" mime:"multipart/form-data" tags:"Admin/Asset" summary:"漫画 zip 批量入库"`
}

type ImportComicsItem struct {
	Id           string `json:"id"`
	Title        string `json:"title"`
	Category     string `json:"category"`
	ChapterCount int    `json:"chapter_count"`
	PageCount    int    `json:"page_count"`
}

type ImportComicsFail struct {
	Title string `json:"title"`
	Error string `json:"error"`
}

type ImportComicsRes struct {
	Imported    int                `json:"imported"`
	FailedCount int                `json:"failed_count"`
	List        []ImportComicsItem `json:"list"`
	Failed      []ImportComicsFail `json:"failed"`
}
