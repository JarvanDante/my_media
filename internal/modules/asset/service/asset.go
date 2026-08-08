package service

import (
	"context"

	"github.com/JarvanDante/my_media/internal/modules/asset/domain"
)

type Asset interface {
	List(ctx context.Context, f domain.ListFilter) (list []domain.Asset, total int, err error)
	Create(ctx context.Context, title, coverUrl, remark string) (id int64, err error)
	Get(ctx context.Context, id int64) (*domain.Asset, error)
	Pick(ctx context.Context, appKey, siteCode string, assetID int64) (*domain.Asset, error)
}
