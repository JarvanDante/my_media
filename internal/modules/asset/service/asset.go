package service

import (
	"context"
	"io"

	v1 "github.com/JarvanDante/my_media/api/admin/asset/v1"
	"github.com/JarvanDante/my_media/internal/modules/asset/domain"
	"github.com/JarvanDante/my_media/internal/shared/transcode"
)

type Asset interface {
	List(ctx context.Context, f domain.ListFilter) (list []domain.Asset, total int, err error)
	Create(ctx context.Context, title, coverUrl, remark string) (code string, err error)
	Get(ctx context.Context, code string) (*domain.Asset, error)
	Delete(ctx context.Context, code string) (deletedObjects int, err error)
	Pick(ctx context.Context, appKey, siteCode, code string) (*domain.Asset, error)

	PresignUpload(ctx context.Context, code, filename string) (*v1.UploadURLRes, error)
	TriggerTranscode(ctx context.Context, code string, coverSeekSec int) (jobID string, err error)
	ReplaceCover(ctx context.Context, code, filename string, body io.Reader, size int64) error
	HandleTranscodeResult(ctx context.Context, msg transcode.ResultMessage) error
	ImportComics(ctx context.Context, zipPath string) (*v1.ImportComicsRes, error)
	ComicChapters(ctx context.Context, code string) ([]v1.ComicChapterItem, error)

	ListPicks(ctx context.Context, appKey string, page, size int) (list []domain.PickRecord, total int, err error)
	PickedSet(ctx context.Context, appKey string, codes []string) (map[string]bool, error)
}
