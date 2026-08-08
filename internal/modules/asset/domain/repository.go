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

type PickRecord struct {
	AssetId     int64
	Title       string
	CoverUrl    string
	PlayUrl     string
	PlayKey     string
	DurationSec int
	PickedAt    string
}

type Repository interface {
	List(ctx context.Context, f ListFilter) (list []Asset, total int, err error)
	Create(ctx context.Context, title, coverUrl, remark string) (id int64, err error)
	Get(ctx context.Context, id int64) (*Asset, error)
	Pick(ctx context.Context, appKey, siteCode string, assetID int64) (*Asset, error)
	ListPicks(ctx context.Context, appKey string, page, size int) (list []PickRecord, total int, err error)
	PickedSet(ctx context.Context, appKey string, assetIDs []int64) (map[int64]bool, error)

	BindSource(ctx context.Context, id int64, bucket, key string) error
	MarkTranscoding(ctx context.Context, id int64, jobID, profile string) error
	ApplyTranscodeResult(ctx context.Context, r TranscodeResult) error
}
