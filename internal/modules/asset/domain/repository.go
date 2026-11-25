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
	Kind            int
	Category        string
	Intro           string
	ChapterCount    int
	WordCount       int
	ParentId        int64
	Seq             int
	UpdateStatus    int
	CreatedAt       string
}

type ListFilter struct {
	Page      int
	Size      int
	Keyword   string
	Status    int // -1 全部
	Kind      int // -1 全部  0视频  1漫画  2动漫  3抖音  4小说  5短剧
	ParentId  int64 // >0 时列出该整部下的剧集；0 时排除剧集
	ReadyOnly bool
	DramaOpen bool // 短剧整部：未下架且至少一集就绪
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

type ComicPage struct {
	Key      string `json:"key"`
	Filename string `json:"filename"`
}

type ComicChapter struct {
	Seq       int
	Title     string
	PageCount int
	Pages     []ComicPage
}

type ComicsCreateInput struct {
	Title    string
	CoverUrl string
	Category string
	Intro    string
	Remark   string
}

type NovelChapter struct {
	Seq       int
	Title     string
	Content   string
	WordCount int
}

type NovelCreateInput struct {
	Title    string
	CoverUrl string
	Category string
	Intro    string
	Remark   string
}

type Repository interface {
	List(ctx context.Context, f ListFilter) (list []Asset, total int, err error)
	Create(ctx context.Context, title, coverUrl, remark string, kind int) (code string, err error)
	CreateComics(ctx context.Context, in ComicsCreateInput) (pk int64, code string, err error)
	FindComicsByTitle(ctx context.Context, title string) (*Asset, error)
	UpdateComicsReady(ctx context.Context, pk int64, bucket, coverKey, coverURL string, chapterCount int) error
	UpdateComicsChapterCount(ctx context.Context, pk int64, chapterCount int) error
	ReplaceComicChapters(ctx context.Context, assetID int64, chapters []ComicChapter) error
	AppendComicChapters(ctx context.Context, assetID int64, chapters []ComicChapter) error
	ListComicChapters(ctx context.Context, assetID int64) ([]ComicChapter, error)
	DeleteComicChapter(ctx context.Context, assetID int64, seq int) error
	CreateNovel(ctx context.Context, in NovelCreateInput) (pk int64, code string, err error)
	FindNovelByTitle(ctx context.Context, title string) (*Asset, error)
	UpdateNovelReady(ctx context.Context, pk int64, bucket, coverKey, coverURL string, chapterCount, wordCount int) error
	UpdateNovelStats(ctx context.Context, pk int64, chapterCount, wordCount int) error
	ReplaceNovelChapters(ctx context.Context, assetID int64, chapters []NovelChapter) error
	AppendNovelChapters(ctx context.Context, assetID int64, chapters []NovelChapter) error
	UpdateNovelChapter(ctx context.Context, assetID int64, ch NovelChapter) error
	ListNovelChapters(ctx context.Context, assetID int64) ([]NovelChapter, error)
	DeleteNovelChapter(ctx context.Context, assetID int64, seq int) error
	GetByCode(ctx context.Context, code string) (*Asset, error)
	GetByPK(ctx context.Context, pk int64) (*Asset, error)
	ReadyEpisodeCounts(ctx context.Context, parentPKs []int64) (map[int64]int, error)
	Delete(ctx context.Context, pk int64) error
	DeleteTree(ctx context.Context, parentPK int64) error
	CreateDrama(ctx context.Context, title, coverUrl, remark, intro string, updateStatus int) (code string, err error)
	UpdateDrama(ctx context.Context, pk int64, title, remark, intro string, updateStatus int) error
	CreateEpisode(ctx context.Context, parentPK int64, seq int, title string) (code string, created bool, err error)
	UpdateEpisodeMeta(ctx context.Context, pk int64, title string, seq int) error
	FindDramasByTitle(ctx context.Context, title string) ([]Asset, error)
	ListEpisodes(ctx context.Context, parentPK int64) ([]Asset, error)
	FindEpisode(ctx context.Context, parentPK int64, seq int) (*Asset, error)
	UpdateChapterCount(ctx context.Context, pk int64, n int) error
	Pick(ctx context.Context, appKey, siteCode, code string) (*Asset, error)
	ListPicks(ctx context.Context, appKey string, page, size int) (list []PickRecord, total int, err error)
	PickedSet(ctx context.Context, appKey string, codes []string) (map[string]bool, error)

	BindSource(ctx context.Context, pk int64, bucket, key string) error
	UpdateCover(ctx context.Context, pk int64, coverURL, coverKey string, kind int) error
	MarkTranscoding(ctx context.Context, pk int64, jobID, profile string) error
	ApplyTranscodeResult(ctx context.Context, r TranscodeResult) error
}
