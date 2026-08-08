package domain

import "context"

type Asset struct {
	Id              int64
	Title           string
	CoverUrl        string
	SourceBucket    string
	SourceKey       string
	PlayKey         string
	PlayUrl         string
	DurationSec     int
	Status          int
	TranscodeStatus string
	TranscodeJobId  string
	TranscodeError  string
	Remark          string
	CreatedAt       string
}

type ListFilter struct {
	Page      int
	Size      int
	Keyword   string
	Status    int // -1 全部
	ReadyOnly bool
}

type TranscodeResult struct {
	JobID       string
	Status      string // processing|ready|failed
	PlayKey     string
	PlayURL     string
	DurationSec int
	Error       string
}

type Repository interface {
	List(ctx context.Context, f ListFilter) (list []Asset, total int, err error)
	Create(ctx context.Context, title, coverUrl, remark string) (id int64, err error)
	Get(ctx context.Context, id int64) (*Asset, error)
	Pick(ctx context.Context, appKey, siteCode string, assetID int64) (*Asset, error)

	BindSource(ctx context.Context, id int64, bucket, key string) error
	MarkTranscoding(ctx context.Context, id int64, jobID, profile string) error
	ApplyTranscodeResult(ctx context.Context, r TranscodeResult) error
}
