package controller

import (
	"context"
	"errors"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"

	v1 "github.com/JarvanDante/my_media/api/open/asset/v1"
	"github.com/JarvanDante/my_media/internal/dao"
	"github.com/JarvanDante/my_media/internal/modules/asset/domain"
	"github.com/JarvanDante/my_media/internal/modules/asset/service"
	"github.com/JarvanDante/my_media/internal/shared/errcode"
)

type Open struct {
	svc service.Asset
}

func NewOpen(svc service.Asset) *Open { return &Open{svc: svc} }

func (c *Open) List(ctx context.Context, req *v1.ListReq) (res *v1.ListRes, err error) {
	list, total, err := c.svc.List(ctx, domain.ListFilter{
		Page: req.Page, Size: req.Size, Keyword: req.Keyword, ReadyOnly: true,
	})
	if err != nil {
		return nil, err
	}
	res = &v1.ListRes{Total: total, List: make([]v1.AssetItem, 0, len(list))}
	for _, a := range list {
		res.List = append(res.List, v1.AssetItem{
			Id: a.Id, Title: a.Title, CoverUrl: a.CoverUrl,
			PlayUrl: a.PlayUrl, DurationSec: a.DurationSec,
		})
	}
	return res, nil
}

func (c *Open) Detail(ctx context.Context, req *v1.DetailReq) (res *v1.DetailRes, err error) {
	a, err := c.svc.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if a == nil || a.Status != 2 {
		return nil, gerror.NewCode(errcode.CodeNotFound, "资产不可用")
	}
	return &v1.DetailRes{
		AssetItem: v1.AssetItem{
			Id: a.Id, Title: a.Title, CoverUrl: a.CoverUrl,
			PlayUrl: a.PlayUrl, DurationSec: a.DurationSec,
		},
		PlayKey: a.PlayKey,
	}, nil
}

func (c *Open) Pick(ctx context.Context, req *v1.PickReq) (res *v1.PickRes, err error) {
	r := g.RequestFromCtx(ctx)
	appKey := r.GetCtxVar("app_key").String()
	siteCode := r.GetCtxVar("site_code").String()
	a, err := c.svc.Pick(ctx, appKey, siteCode, req.Id)
	if err != nil {
		if errors.Is(err, dao.ErrAssetNotReady) {
			return nil, gerror.NewCode(errcode.CodeBadRequest, "资产未就绪")
		}
		return nil, err
	}
	if a == nil {
		return nil, gerror.NewCode(errcode.CodeNotFound, "资产不存在")
	}
	return &v1.PickRes{AssetId: a.Id, PlayUrl: a.PlayUrl}, nil
}
