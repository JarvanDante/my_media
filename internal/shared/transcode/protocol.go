// Package transcode 与 my_transcode Kafka 协议对齐(字段兼容)。
package transcode

import "strconv"

const (
	TopicJobs    = "media.transcode.jobs"
	TopicResults = "media.transcode.results"

	StatusProcessing = "processing"
	StatusReady      = "ready"
	StatusFailed     = "failed"

	ProfileH264HLS = "h264_hls"
	BizMedia       = "media"
)

type ObjectRef struct {
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
}

type OutputRef struct {
	Bucket string `json:"bucket"`
	Prefix string `json:"prefix"`
}

// JobMessage 转码任务
type JobMessage struct {
	SchemaVersion int       `json:"schema_version"`
	JobID         string    `json:"job_id"`
	Biz           string    `json:"biz"`
	BizRef        string    `json:"biz_ref"`
	Input         ObjectRef `json:"input"`
	Output        OutputRef `json:"output"`
	Profile       string    `json:"profile"`
	CoverSeekSec  int       `json:"cover_seek_sec,omitempty"`
	CreatedAt     string    `json:"created_at,omitempty"`
}

// ResultMessage 转码结果
type ResultMessage struct {
	SchemaVersion int    `json:"schema_version"`
	JobID         string `json:"job_id"`
	Biz           string `json:"biz"`
	BizRef        string `json:"biz_ref"`
	Status        string `json:"status"`
	PlayKey       string `json:"play_key,omitempty"`
	PlayURL       string `json:"play_url,omitempty"`
	CoverKey      string `json:"cover_key,omitempty"`
	CoverURL      string `json:"cover_url,omitempty"`
	DurationSec   int    `json:"duration_sec,omitempty"`
	Error         string `json:"error,omitempty"`
	FinishedAt    string `json:"finished_at,omitempty"`
}

// BizRefAsset 生成 biz_ref，如 asset:12
func BizRefAsset(assetID int64) string {
	return "asset:" + strconv.FormatInt(assetID, 10)
}
