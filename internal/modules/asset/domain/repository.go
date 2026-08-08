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

type Repository interface {
	List(ctx context.Context, f ListFilter) (list []Asset, total int, err error)
	Create(ctx context.Context, title, coverUrl, remark string) (id int64, err error)
	Get(ctx context.Context, id int64) (*Asset, error)
	Pick(ctx context.Context, appKey, siteCode string, assetID int64) (*Asset, error)
}
