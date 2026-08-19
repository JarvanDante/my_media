package controller

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"

	adminv1 "github.com/JarvanDante/my_media/api/admin/asset/v1"
	v1 "github.com/JarvanDante/my_media/api/open/asset/v1"
	"github.com/JarvanDante/my_media/internal/consts"
	"github.com/JarvanDante/my_media/internal/dao"
	"github.com/JarvanDante/my_media/internal/modules/asset/domain"
	"github.com/JarvanDante/my_media/internal/modules/asset/service"
	"github.com/JarvanDante/my_media/internal/shared/errcode"
	"github.com/JarvanDante/my_media/internal/shared/playsign"
	"github.com/JarvanDante/my_media/internal/shared/ratelimit"
)

type Open struct {
	svc service.Asset
}

func NewOpen(svc service.Asset) *Open { return &Open{svc: svc} }

func (c *Open) List(ctx context.Context, req *v1.ListReq) (res *v1.ListRes, err error) {
	kind := req.Kind
	if kind < 0 {
		kind = consts.KindVideo
	}
	list, total, err := c.svc.List(ctx, domain.ListFilter{
		Page: req.Page, Size: req.Size, Keyword: req.Keyword, ReadyOnly: true, Kind: kind,
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
		res.List = append(res.List, toOpenItem(a, picked[a.Code], siteCode))
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
	item := toOpenItem(*a, picked[a.Code], siteCode)
	res = &v1.DetailRes{AssetItem: item, PlayKey: a.PlayKey, Intro: a.Intro}
	if a.Kind == consts.KindComics {
		chs, e := c.svc.ComicChapters(ctx, a.Code)
		if e != nil {
			return nil, e
		}
		res.Chapters = toOpenChapters(chs)
	}
	return res, nil
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
	item := toOpenItem(*a, true, siteCode)
	res = &v1.PickRes{
		Id: a.Code, Title: a.Title, CoverUrl: item.CoverUrl, PlayUrl: item.PlayUrl,
		PlayKey: a.PlayKey, DurationSec: a.DurationSec, Kind: a.Kind, Intro: a.Intro,
		ChapterCount: a.ChapterCount,
	}
	if a.Kind == consts.KindComics {
		chs, e := c.svc.ComicChapters(ctx, a.Code)
		if e != nil {
			return nil, e
		}
		res.Chapters = toOpenChapters(chs)
	}
	return res, nil
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
			Id: x.Code, Title: x.Title, CoverUrl: playsign.WrapCover(x.Code, x.CoverUrl, siteCode),
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
	// 平台级防刷: 按 app_key(站点)统计签发频率, 超限直接 429 + 告警日志。
	// 注意: app_key 是站点级凭证, 这里是"每站点聚合上限", 拦不住站内单个用户;
	// 站内按终端用户限流应在站点后端(my_service)做。
	rlKey := appKey
	if rlKey == "" {
		rlKey = r.GetClientIp()
	}
	if ok, retry, reason := ratelimit.Default(ctx).Allow("playtoken:"+rlKey, time.Now().Unix()); !ok {
		g.Log().Warningf(ctx, "play-token 限流命中 app_key=%s site=%s ip=%s asset=%s: %s",
			appKey, siteCode, r.GetClientIp(), a.Code, reason)
		r.Response.Header().Set("Retry-After", strconv.Itoa(retry))
		r.Response.WriteStatus(http.StatusTooManyRequests)
		r.Response.WriteJsonExit(g.Map{"code": 429, "message": reason, "data": nil})
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

func toOpenItem(a domain.Asset, picked bool, siteCode string) v1.AssetItem {
	cover := playsign.WrapCover(a.Code, a.CoverUrl, siteCode)
	play := a.PlayUrl
	if a.Kind != consts.KindComics {
		play = playsign.Wrap(a.Code, a.PlayUrl, siteCode)
	}
	return v1.AssetItem{
		Id: a.Code, Title: a.Title, CoverUrl: cover, PlayUrl: play,
		DurationSec: a.DurationSec, Kind: a.Kind, Intro: a.Intro,
		ChapterCount: a.ChapterCount, Picked: picked,
	}
}

func toOpenChapters(chs []adminv1.ComicChapterItem) []v1.ComicChapterItem {
	out := make([]v1.ComicChapterItem, 0, len(chs))
	for _, ch := range chs {
		pages := make([]v1.ComicPageItem, 0, len(ch.Pages))
		for _, p := range ch.Pages {
			pages = append(pages, v1.ComicPageItem{Filename: p.Filename, Key: p.Key, Url: p.Url})
		}
		out = append(out, v1.ComicChapterItem{
			Seq: ch.Seq, Title: ch.Title, PageCount: ch.PageCount, Pages: pages,
		})
	}
	return out
}
