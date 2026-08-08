package domain

import "context"

type Asset struct {
	Pk              int64  // 内部自增主键
	Code            string // 对外短码(新 16 位，历史可能 8 位)
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
	CoverURL    string
	DurationSec int
	Error       string
}

type PickRecord struct {
	Code        string
	Title       string
	CoverUrl    string
	PlayUrl     string
	PlayKey     string
	DurationSec int
	PickedAt    string
}

type Repository interface {
	List(ctx context.Context, f ListFilter) (list []Asset, total int, err error)
	Create(ctx context.Context, title, coverUrl, remark string) (code string, err error)
	GetByCode(ctx context.Context, code string) (*Asset, error)
	Delete(ctx context.Context, pk int64) error
	Pick(ctx context.Context, appKey, siteCode, code string) (*Asset, error)
	ListPicks(ctx context.Context, appKey string, page, size int) (list []PickRecord, total int, err error)
	PickedSet(ctx context.Context, appKey string, codes []string) (map[string]bool, error)

	BindSource(ctx context.Context, pk int64, bucket, key string) error
	MarkTranscoding(ctx context.Context, pk int64, jobID, profile string) error
	ApplyTranscodeResult(ctx context.Context, r TranscodeResult) error
}
