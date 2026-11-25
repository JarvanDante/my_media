package logic

import (
	"context"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/JarvanDante/my_media/internal/consts"
	"github.com/JarvanDante/my_media/internal/modules/asset/domain"
	"github.com/JarvanDante/my_media/internal/shared/errcode"
)

// OpenDrama 开放接口取短剧整部及其就绪剧集；整部下架或无就绪剧集按不可用处理。
func (s *sAsset) OpenDrama(ctx context.Context, code string) (*domain.Asset, []domain.Asset, error) {
	a, err := s.Get(ctx, code)
	if err != nil {
		return nil, nil, err
	}
	if a == nil || !consts.IsDramaSeries(a.Kind, a.ParentId) || a.Status == consts.AssetStatusOff {
		return nil, nil, gerror.NewCode(errcode.CodeNotFound, "资产不可用")
	}
	list, err := s.repo.ListEpisodes(ctx, a.Pk)
	if err != nil {
		return nil, nil, err
	}
	eps := make([]domain.Asset, 0, len(list))
	for i := range list {
		if list[i].Status != consts.AssetStatusReady {
			continue
		}
		s.decorateCover(ctx, &list[i])
		eps = append(eps, list[i])
	}
	if len(eps) == 0 {
		return nil, nil, gerror.NewCode(errcode.CodeNotFound, "资产不可用")
	}
	return a, eps, nil
}

// OpenDramaEpisode 取可签发的剧集及其整部；剧集未就绪或整部下架都按不可用处理。
func (s *sAsset) OpenDramaEpisode(ctx context.Context, code string) (*domain.Asset, *domain.Asset, error) {
	ep, err := s.repo.GetByCode(ctx, code)
	if err != nil {
		return nil, nil, err
	}
	if ep == nil || !consts.IsDramaEpisode(ep.Kind, ep.ParentId) || ep.Status != consts.AssetStatusReady {
		return nil, nil, gerror.NewCode(errcode.CodeNotFound, "资产不可用")
	}
	parent, err := s.repo.GetByPK(ctx, ep.ParentId)
	if err != nil {
		return nil, nil, err
	}
	if parent == nil || !consts.IsDramaSeries(parent.Kind, parent.ParentId) || parent.Status == consts.AssetStatusOff {
		return nil, nil, gerror.NewCode(errcode.CodeNotFound, "资产不可用")
	}
	return ep, parent, nil
}

func (s *sAsset) ReadyEpisodeCounts(ctx context.Context, parentPKs []int64) (map[int64]int, error) {
	return s.repo.ReadyEpisodeCounts(ctx, parentPKs)
}
