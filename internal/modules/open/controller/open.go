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
	"github.com/JarvanDante/my_media/internal/shared/playsign"
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
	codes := make([]string, 0, len(list))
	for _, a := range list {
		codes = append(codes, a.Code)
	}
	r := g.RequestFromCtx(ctx)
	appKey := r.GetCtxVar("app_key").String()
	siteCode := r.GetCtxVar("site_code").String()
	picked, err := c.svc.PickedSet(ctx, appKey, codes)
	if err != nil {
		return nil, err
	}
	res = &v1.ListRes{Total: total, List: make([]v1.AssetItem, 0, len(list))}
	for _, a := range list {
		res.List = append(res.List, v1.AssetItem{
			Id: a.Code, Title: a.Title, CoverUrl: a.CoverUrl,
			PlayUrl: playsign.Wrap(a.Code, a.PlayUrl, siteCode), DurationSec: a.DurationSec, Picked: picked[a.Code],
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
	r := g.RequestFromCtx(ctx)
	appKey := r.GetCtxVar("app_key").String()
	siteCode := r.GetCtxVar("site_code").String()
	picked, err := c.svc.PickedSet(ctx, appKey, []string{a.Code})
	if err != nil {
		return nil, err
	}
	return &v1.DetailRes{
		AssetItem: v1.AssetItem{
			Id: a.Code, Title: a.Title, CoverUrl: a.CoverUrl,
			PlayUrl: playsign.Wrap(a.Code, a.PlayUrl, siteCode), DurationSec: a.DurationSec, Picked: picked[a.Code],
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
	return &v1.PickRes{
		Id: a.Code, Title: a.Title, CoverUrl: a.CoverUrl,
		PlayUrl: playsign.Wrap(a.Code, a.PlayUrl, siteCode), PlayKey: a.PlayKey, DurationSec: a.DurationSec,
	}, nil
}

func (c *Open) PickList(ctx context.Context, req *v1.PickListReq) (res *v1.PickListRes, err error) {
	r := g.RequestFromCtx(ctx)
	appKey := r.GetCtxVar("app_key").String()
	siteCode := r.GetCtxVar("site_code").String()
	list, total, err := c.svc.ListPicks(ctx, appKey, req.Page, req.Size)
	if err != nil {
		return nil, err
	}
	res = &v1.PickListRes{Total: total, List: make([]v1.PickListItem, 0, len(list))}
	for _, x := range list {
		res.List = append(res.List, v1.PickListItem{
			Id: x.Code, Title: x.Title, CoverUrl: x.CoverUrl,
			PlayUrl: playsign.Wrap(x.Code, x.PlayUrl, siteCode), PlayKey: x.PlayKey, DurationSec: x.DurationSec, PickedAt: x.PickedAt,
		})
	}
	return res, nil
}

// PlayToken 为已选用的就绪资产签发播放地址(策略 TTL / 试看 / IP 绑定)。
func (c *Open) PlayToken(ctx context.Context, req *v1.PlayTokenReq) (res *v1.PlayTokenRes, err error) {
	a, err := c.svc.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if a == nil || a.Status != 2 {
		return nil, gerror.NewCode(errcode.CodeNotFound, "资产不可用")
	}
	r := g.RequestFromCtx(ctx)
	appKey := r.GetCtxVar("app_key").String()
	siteCode := r.GetCtxVar("site_code").String()
	picked, err := c.svc.PickedSet(ctx, appKey, []string{a.Code})
	if err != nil {
		return nil, err
	}
	if !picked[a.Code] {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "请先选用该媒资")
	}
	if !playsign.Enabled() {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "播放网关未配置")
	}
	var ttl int64
	if p, _ := dao.NewPlayRepo().PolicyGet(ctx, siteCode); p != nil && p.Status == 1 && p.TokenTtlSec > 0 {
		ttl = int64(p.TokenTtlSec)
	}
	playURL, exp := playsign.SignURL(a.Code, siteCode, ttl, req.PreviewSec, req.ClientIp, a.PlayUrl)
	return &v1.PlayTokenRes{PlayUrl: playURL, ExpiresAt: exp}, nil
}
