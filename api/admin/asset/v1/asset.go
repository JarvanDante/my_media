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
	ParentId        int64  `json:"parent_id,omitempty"`
	Seq             int    `json:"seq,omitempty"`
	UpdateStatus    int    `json:"update_status,omitempty"`
	Intro           string `json:"intro,omitempty"`
	Category        string `json:"category"`
	ChapterCount    int    `json:"chapter_count"`
	WordCount       int    `json:"word_count,omitempty"`
	CreatedAt       string `json:"created_at"`
}

type ListReq struct {
	g.Meta  `path:"/admin/assets" method:"get" tags:"Admin/Asset" summary:"媒资池列表"`
	Page    int    `json:"page" d:"1"`
	Size    int    `json:"size" d:"20"`
	Keyword string `json:"keyword"`
	Status  int    `json:"status" d:"-1"`
	Kind    int    `json:"kind" d:"-1"` // -1全部 0视频 1漫画 2动漫 3抖音 4小说 5短剧
}

type ListRes struct {
	List  []AssetItem `json:"list"`
	Total int         `json:"total"`
}

type CreateReq struct {
	g.Meta       `path:"/admin/assets" method:"post" tags:"Admin/Asset" summary:"创建资产元数据"`
	Title        string `json:"title" v:"required#标题必填"`
	CoverUrl     string `json:"cover_url"`
	Remark       string `json:"remark"`
	Kind         int    `json:"kind" d:"0"` // 0视频 2动漫 3抖音 5短剧；漫画/小说请走 zip 导入
	Intro        string `json:"intro"`
	UpdateStatus int    `json:"update_status"`
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
	SourceBucket   string             `json:"source_bucket"`
	SourceKey      string             `json:"source_key"`
	PlayKey        string             `json:"play_key"`
	TranscodeJobId string             `json:"transcode_job_id"`
	TranscodeError string             `json:"transcode_error"`
	Remark         string             `json:"remark"`
	Chapters       []ComicChapterItem `json:"chapters,omitempty"`
	Episodes       []DramaEpisodeItem `json:"episodes,omitempty"`
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

type ReplaceCoverReq struct {
	g.Meta `path:"/admin/assets/{id}/cover" method:"post" mime:"multipart/form-data" tags:"Admin/Asset" summary:"替换封面(不转码)"`
	Id     string `json:"id" in:"path" v:"required|length:8,16"`
}

type ReplaceCoverRes struct {
	CoverUrl string `json:"cover_url"`
}

type DeleteReq struct {
	g.Meta `path:"/admin/assets/{id}" method:"delete" tags:"Admin/Asset" summary:"删除资产并清理对象存储"`
	Id     string `json:"id" in:"path" v:"required|length:8,16"`
}

type DeleteRes struct {
	DeletedObjects int `json:"deleted_objects"`
}

type DeleteChapterReq struct {
	g.Meta `path:"/admin/assets/{id}/chapters/{seq}" method:"delete" tags:"Admin/Asset" summary:"删除漫画/小说单章"`
	Id     string `json:"id" in:"path" v:"required|length:8,16"`
	Seq    int    `json:"seq" in:"path" v:"required|min:1"`
}

type DeleteChapterRes struct {
	DeletedObjects int `json:"deleted_objects"`
	ChapterCount   int `json:"chapter_count"`
}

type CreateNovelChapterReq struct {
	g.Meta  `path:"/admin/assets/{id}/novel-chapters" method:"post" tags:"Admin/Asset" summary:"新增小说章节"`
	Id      string `json:"id" in:"path" v:"required|length:8,16"`
	Seq     int    `json:"seq"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

type CreateNovelChapterRes struct {
	Seq          int `json:"seq"`
	WordCount    int `json:"word_count"`
	ChapterCount int `json:"chapter_count"`
}

type UpdateNovelChapterReq struct {
	g.Meta  `path:"/admin/assets/{id}/novel-chapters/{seq}" method:"put" tags:"Admin/Asset" summary:"编辑小说章节"`
	Id      string `json:"id" in:"path" v:"required|length:8,16"`
	Seq     int    `json:"seq" in:"path" v:"required|min:1"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

type UpdateNovelChapterRes struct {
	Seq          int `json:"seq"`
	WordCount    int `json:"word_count"`
	ChapterCount int `json:"chapter_count"`
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
	Pages     []ComicPageItem `json:"pages,omitempty"`
	Content   string          `json:"content,omitempty"`
	WordCount int             `json:"word_count,omitempty"`
}

type ImportComicsReq struct {
	g.Meta `path:"/admin/comics/import" method:"post" mime:"multipart/form-data" tags:"Admin/Asset" summary:"漫画 zip 批量入库"`
	Resume bool `json:"resume" p:"resume" dc:"续载：同名漫画追加新话，已有话数跳过"`
}

type ImportComicsItem struct {
	Id           string `json:"id"`
	Title        string `json:"title"`
	Category     string `json:"category"`
	ChapterCount int    `json:"chapter_count"`
	PageCount    int    `json:"page_count"`
	Appended     bool   `json:"appended"`
	SkippedCount int    `json:"skipped_count"`
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

type ImportNovelsReq struct {
	g.Meta `path:"/admin/novels/import" method:"post" mime:"multipart/form-data" tags:"Admin/Asset" summary:"小说 zip 批量入库"`
	Resume bool `json:"resume" p:"resume" dc:"续载：同名小说追加新章，已有章序跳过"`
}

type ImportNovelsItem struct {
	Id           string `json:"id"`
	Title        string `json:"title"`
	Category     string `json:"category"`
	ChapterCount int    `json:"chapter_count"`
	WordCount    int    `json:"word_count"`
	Appended     bool   `json:"appended"`
	SkippedCount int    `json:"skipped_count"`
}

type ImportNovelsFail struct {
	Title string `json:"title"`
	Error string `json:"error"`
}

type ImportNovelsRes struct {
	Imported    int                `json:"imported"`
	FailedCount int                `json:"failed_count"`
	List        []ImportNovelsItem `json:"list"`
	Failed      []ImportNovelsFail `json:"failed"`
}

type DramaEpisodeItem struct {
	Id              string `json:"id"`
	Seq             int    `json:"seq"`
	Title           string `json:"title"`
	Status          int    `json:"status"`
	TranscodeStatus string `json:"transcode_status"`
	PlayUrl         string `json:"play_url"`
	DurationSec     int    `json:"duration_sec"`
	SourceKey       string `json:"source_key"`
	CoverUrl        string `json:"cover_url,omitempty"`
	TranscodeError  string `json:"transcode_error,omitempty"`
}

type UpdateAssetReq struct {
	g.Meta       `path:"/admin/assets/{id}" method:"put" tags:"Admin/Asset" summary:"编辑短剧整部或剧集资料"`
	Id           string `json:"id" in:"path" v:"required|length:8,16"`
	Title        string `json:"title"`
	Remark       string `json:"remark"`
	Intro        string `json:"intro"`
	UpdateStatus int    `json:"update_status"`
	Seq          int    `json:"seq"`
}

type UpdateAssetRes struct {
	Id string `json:"id"`
}

type CreateEpisodeReq struct {
	g.Meta `path:"/admin/assets/{id}/episodes" method:"post" tags:"Admin/Asset" summary:"新增短剧剧集(按集号幂等)"`
	Id     string `json:"id" in:"path" v:"required|length:8,16"`
	Seq    int    `json:"seq" v:"required|min:1#集号必须从1开始"`
	Title  string `json:"title"`
}

type CreateEpisodeRes struct {
	Id      string `json:"id"`
	Created bool   `json:"created"`
}

type DramaImportLimitsReq struct {
	g.Meta `path:"/admin/dramas/import-limits" method:"get" tags:"Admin/Asset" summary:"短剧zip导入上限"`
}

type DramaImportLimitsRes struct {
	MaxZipBytes      int64   `json:"max_zip_bytes"`
	MaxBodyBytes     int64   `json:"max_body_bytes"`
	MaxChunkBytes    int64   `json:"max_chunk_bytes"`
	MaxParts         int     `json:"max_parts"`
	MaxExtractBytes  int64   `json:"max_extract_bytes"`
	MaxFiles         int     `json:"max_files"`
	MaxFileBytes     int64   `json:"max_file_bytes"`
	MaxRatio         float64 `json:"max_ratio"`
	MinFreeBytes     int64   `json:"min_free_bytes"`
	ImportTimeoutSec int     `json:"import_timeout_sec"`
	JobTimeoutSec    int     `json:"job_timeout_sec"`
	MaxRunningJobs   int     `json:"max_running_jobs"`
}

type ImportDramasReq struct {
	g.Meta   `path:"/admin/dramas/import" method:"post" mime:"multipart/form-data" tags:"Admin/Asset" summary:"短剧 zip 兼容整包入库（小包，异步任务）"`
	Resume   bool   `json:"resume" p:"resume" dc:"已废弃：不得按标题混剧"`
	Plan     string `json:"plan" p:"plan" dc:"可选，JSON 导入计划"`
	TargetId string `json:"target_id" p:"target_id" dc:"显式指定续传目标短剧 id；空则创建新剧"`
}

type ImportDramasItem struct {
	Id            string `json:"id"`
	Title         string `json:"title"`
	ChapterCount  int    `json:"chapter_count"`
	Appended      bool   `json:"appended"`
	SkippedCount  int    `json:"skipped_count"`
	ImportedCount int    `json:"imported_count"`
}

type ImportDramasFail struct {
	Title string `json:"title"`
	Error string `json:"error"`
}

type ImportDramasRes struct {
	JobId       string             `json:"job_id,omitempty"`
	Imported    int                `json:"imported"`
	FailedCount int                `json:"failed_count"`
	List        []ImportDramasItem `json:"list"`
	Failed      []ImportDramasFail `json:"failed"`
}

type DramaUploadPart struct {
	N      int    `json:"n"`
	Offset int64  `json:"offset"`
	Size   int64  `json:"size"`
	Done   bool   `json:"done"`
	Mode   string `json:"mode"` // presign | proxy
	Url    string `json:"url,omitempty"`
}

type CreateDramaUploadReq struct {
	g.Meta    `path:"/admin/dramas/uploads" method:"post" tags:"Admin/Asset" summary:"创建短剧 zip 分片上传会话"`
	Filename  string `json:"filename" v:"required"`
	TotalSize int64  `json:"total_size" v:"required|min:1"`
}

type DramaUploadRes struct {
	Id        string            `json:"id"`
	Filename  string            `json:"filename"`
	TotalSize int64             `json:"total_size"`
	ChunkSize int64             `json:"chunk_size"`
	PartCount int               `json:"part_count"`
	Status    string            `json:"status"`
	ExpiresAt string            `json:"expires_at"`
	JobId     string            `json:"job_id,omitempty"`
	Parts     []DramaUploadPart `json:"parts"`
}

type GetDramaUploadReq struct {
	g.Meta `path:"/admin/dramas/uploads/{id}" method:"get" tags:"Admin/Asset" summary:"查询短剧 zip 分片上传会话"`
	Id     string `json:"id" in:"path" v:"required|length:8,16"`
}

type PutDramaUploadPartReq struct {
	g.Meta `path:"/admin/dramas/uploads/{id}/parts/{n}" method:"put" mime:"application/octet-stream" tags:"Admin/Asset" summary:"代传短剧 zip 分片"`
	Id     string `json:"id" in:"path" v:"required|length:8,16"`
	N      int    `json:"n" in:"path" v:"required|min:1"`
}

type PutDramaUploadPartRes struct {
	N    int   `json:"n"`
	Size int64 `json:"size"`
	Done bool  `json:"done"`
}

type CompleteDramaUploadReq struct {
	g.Meta   `path:"/admin/dramas/uploads/{id}/complete" method:"post" tags:"Admin/Asset" summary:"完成分片并创建异步导入任务"`
	Id       string `json:"id" in:"path" v:"required|length:8,16"`
	Plan     string `json:"plan" dc:"可选，JSON 导入计划"`
	TargetId string `json:"target_id" dc:"显式指定续传目标短剧 id；空则创建新剧"`
}

type CompleteDramaUploadRes struct {
	UploadId string `json:"upload_id"`
	JobId    string `json:"job_id"`
	Status   string `json:"status"`
}

type AbortDramaUploadReq struct {
	g.Meta `path:"/admin/dramas/uploads/{id}/abort" method:"post" tags:"Admin/Asset" summary:"取消分片会话并清理临时对象"`
	Id     string `json:"id" in:"path" v:"required|length:8,16"`
}

type AbortDramaUploadRes struct {
	Id      string `json:"id"`
	Deleted int    `json:"deleted"`
}

type DramaImportJobDrama struct {
	Id              string `json:"id"`
	Title           string `json:"title"`
	ChapterCount    int    `json:"chapter_count"`
	Appended        bool   `json:"appended"`
	SkippedCount    int    `json:"skipped_count"`
	ImportedCount   int    `json:"imported_count"`
	ReadyCount      int    `json:"ready_count"`
	ProcessingCount int    `json:"processing_count"`
	FailedCount     int    `json:"failed_count"`
}

type GetDramaImportJobReq struct {
	g.Meta `path:"/admin/dramas/import-jobs/{id}" method:"get" tags:"Admin/Asset" summary:"查询短剧异步导入任务"`
	Id     string `json:"id" in:"path" v:"required|length:8,16"`
}

type DramaImportJobRes struct {
	Id                string                `json:"id"`
	UploadId          string                `json:"upload_id"`
	Status            string                `json:"status"`
	Stage             string                `json:"stage"`
	Progress          int                   `json:"progress"`
	Message           string                `json:"message"`
	Error             string                `json:"error"`
	Attempts          int                   `json:"attempts"`
	MaxAttempts       int                   `json:"max_attempts"`
	Imported          int                   `json:"imported"`
	FailedCount       int                   `json:"failed_count"`
	List              []DramaImportJobDrama `json:"list"`
	Failed            []ImportDramasFail    `json:"failed"`
	EpisodeTotal      int                   `json:"episode_total"`
	EpisodeReady      int                   `json:"episode_ready"`
	EpisodeProcessing int                   `json:"episode_processing"`
	EpisodeFailed     int                   `json:"episode_failed"`
	CreatedAt         string                `json:"created_at"`
	UpdatedAt         string                `json:"updated_at"`
	FinishedAt        string                `json:"finished_at,omitempty"`
}

type RetryDramaImportJobReq struct {
	g.Meta `path:"/admin/dramas/import-jobs/{id}/retry" method:"post" tags:"Admin/Asset" summary:"重试失败的导入或转码投递"`
	Id     string `json:"id" in:"path" v:"required|length:8,16"`
}
