package logic

import (
	"context"

	"github.com/JarvanDante/my_media/internal/modules/asset/domain"
	"github.com/JarvanDante/my_media/internal/modules/asset/service"
)

type sAsset struct {
	repo domain.Repository
}

func New(repo domain.Repository) service.Asset {
	return &sAsset{repo: repo}
}

func (s *sAsset) List(ctx context.Context, f domain.ListFilter) ([]domain.Asset, int, error) {
	return s.repo.List(ctx, f)
}

func (s *sAsset) Create(ctx context.Context, title, coverUrl, remark string) (int64, error) {
	return s.repo.Create(ctx, title, coverUrl, remark)
}

func (s *sAsset) Get(ctx context.Context, id int64) (*domain.Asset, error) {
	return s.repo.Get(ctx, id)
}

func (s *sAsset) Pick(ctx context.Context, appKey, siteCode string, assetID int64) (*domain.Asset, error) {
	return s.repo.Pick(ctx, appKey, siteCode, assetID)
}
