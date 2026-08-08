package service

import (
	"context"

	v1 "github.com/JarvanDante/my_media/api/admin/asset/v1"
	"github.com/JarvanDante/my_media/internal/modules/asset/domain"
	"github.com/JarvanDante/my_media/internal/shared/transcode"
)

type Asset interface {
	List(ctx context.Context, f domain.ListFilter) (list []domain.Asset, total int, err error)
	Create(ctx context.Context, title, coverUrl, remark string) (id int64, err error)
	Get(ctx context.Context, id int64) (*domain.Asset, error)
	Pick(ctx context.Context, appKey, siteCode string, assetID int64) (*domain.Asset, error)

	PresignUpload(ctx context.Context, assetID int64, filename string) (*v1.UploadURLRes, error)
	TriggerTranscode(ctx context.Context, assetID int64) (jobID string, err error)
	HandleTranscodeResult(ctx context.Context, msg transcode.ResultMessage) error
}
