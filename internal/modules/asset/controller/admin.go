package controller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/net/ghttp"

	v1 "github.com/JarvanDante/my_media/api/admin/asset/v1"
	"github.com/JarvanDante/my_media/internal/consts"
	"github.com/JarvanDante/my_media/internal/modules/asset/domain"
	"github.com/JarvanDante/my_media/internal/modules/asset/service"
	"github.com/JarvanDante/my_media/internal/shared/errcode"
	"github.com/JarvanDante/my_media/internal/shared/playsign"
)

type Admin struct {
	svc service.Asset
}

func NewAdmin(svc service.Asset) *Admin { return &Admin{svc: svc} }

func (c *Admin) List(ctx context.Context, req *v1.ListReq) (res *v1.ListRes, err error) {
	list, total, err := c.svc.List(ctx, domain.ListFilter{
		Page: req.Page, Size: req.Size, Keyword: req.Keyword, Status: req.Status, Kind: req.Kind,
	})
	if err != nil {
		return nil, err
	}
	res = &v1.ListRes{Total: total, List: make([]v1.AssetItem, 0, len(list))}
	for _, a := range list {
		res.List = append(res.List, toAdminItem(a))
	}
	return res, nil
}

func (c *Admin) Create(ctx context.Context, req *v1.CreateReq) (res *v1.CreateRes, err error) {
	var code string
	var err2 error
	if req.Kind == consts.KindDrama {
		code, err2 = c.svc.CreateDrama(ctx, req.Title, req.CoverUrl, req.Remark, req.Intro, req.UpdateStatus)
	} else {
		code, err2 = c.svc.Create(ctx, req.Title, req.CoverUrl, req.Remark, req.Kind)
	}
	if err2 != nil {
		return nil, err2
	}
	return &v1.CreateRes{Id: code}, nil
}

func (c *Admin) Detail(ctx context.Context, req *v1.DetailReq) (res *v1.DetailRes, err error) {
	a, err := c.svc.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, gerror.NewCode(errcode.CodeNotFound, "资产不存在")
	}
	res = &v1.DetailRes{
		AssetItem:      toAdminItem(*a),
		SourceBucket:   a.SourceBucket,
		SourceKey:      a.SourceKey,
		PlayKey:        a.PlayKey,
		TranscodeJobId: a.TranscodeJobId,
		TranscodeError: a.TranscodeError,
		Remark:         a.Remark,
	}
	if consts.IsDramaSeries(a.Kind, a.ParentId) {
		eps, e := c.svc.ListEpisodes(ctx, a.Code)
		if e != nil {
			return nil, e
		}
		res.Episodes = make([]v1.DramaEpisodeItem, 0, len(eps))
		for _, ep := range eps {
			item := toAdminItem(ep)
			res.Episodes = append(res.Episodes, v1.DramaEpisodeItem{
				Id: ep.Code, Seq: ep.Seq, Title: ep.Title, Status: ep.Status,
				TranscodeStatus: ep.TranscodeStatus, PlayUrl: item.PlayUrl,
				DurationSec: ep.DurationSec, SourceKey: ep.SourceKey,
				CoverUrl: item.CoverUrl, TranscodeError: ep.TranscodeError,
			})
		}
	}
	if a.Kind == consts.KindComics {
		chs, e := c.svc.ComicChapters(ctx, a.Code)
		if e != nil {
			return nil, e
		}
		res.Chapters = wrapAdminComicPages(a.Code, chs)
	}
	if a.Kind == consts.KindNovel {
		chs, e := c.svc.NovelChapters(ctx, a.Code)
		if e != nil {
			return nil, e
		}
		res.Chapters = chs
	}
	return res, nil
}

func (c *Admin) UploadURL(ctx context.Context, req *v1.UploadURLReq) (res *v1.UploadURLRes, err error) {
	return c.svc.PresignUpload(ctx, req.Id, req.Filename)
}

func (c *Admin) Transcode(ctx context.Context, req *v1.TranscodeReq) (res *v1.TranscodeRes, err error) {
	jobID, err := c.svc.TriggerTranscode(ctx, req.Id, req.CoverSeekSec)
	if err != nil {
		return nil, err
	}
	return &v1.TranscodeRes{JobId: jobID}, nil
}

const maxCoverUpload = 8 << 20

func (c *Admin) ReplaceCover(ctx context.Context, req *v1.ReplaceCoverReq) (res *v1.ReplaceCoverRes, err error) {
	r := ghttp.RequestFromCtx(ctx)
	up := r.GetUploadFile("file")
	if up == nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "请选择封面图片")
	}
	if up.Size > maxCoverUpload {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "封面不能超过 8MB")
	}
	f, err := up.Open()
	if err != nil {
		return nil, gerror.Wrap(err, "读取封面失败")
	}
	defer f.Close()
	if err := c.svc.ReplaceCover(ctx, req.Id, up.Filename, f, up.Size); err != nil {
		return nil, err
	}
	a, err := c.svc.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	cover := ""
	if a != nil {
		cover = playsign.WrapCover(a.Code, a.CoverUrl, "admin")
	}
	return &v1.ReplaceCoverRes{CoverUrl: cover}, nil
}

func (c *Admin) Delete(ctx context.Context, req *v1.DeleteReq) (res *v1.DeleteRes, err error) {
	n, err := c.svc.Delete(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return &v1.DeleteRes{DeletedObjects: n}, nil
}

func (c *Admin) DeleteChapter(ctx context.Context, req *v1.DeleteChapterReq) (res *v1.DeleteChapterRes, err error) {
	n, count, err := c.svc.DeleteChapter(ctx, req.Id, req.Seq)
	if err != nil {
		return nil, err
	}
	return &v1.DeleteChapterRes{DeletedObjects: n, ChapterCount: count}, nil
}

func (c *Admin) CreateNovelChapter(ctx context.Context, req *v1.CreateNovelChapterReq) (res *v1.CreateNovelChapterRes, err error) {
	seq, wc, count, err := c.svc.CreateNovelChapter(ctx, req.Id, req.Seq, req.Title, req.Content)
	if err != nil {
		return nil, err
	}
	return &v1.CreateNovelChapterRes{Seq: seq, WordCount: wc, ChapterCount: count}, nil
}

func (c *Admin) UpdateNovelChapter(ctx context.Context, req *v1.UpdateNovelChapterReq) (res *v1.UpdateNovelChapterRes, err error) {
	wc, count, err := c.svc.UpdateNovelChapter(ctx, req.Id, req.Seq, req.Title, req.Content)
	if err != nil {
		return nil, err
	}
	return &v1.UpdateNovelChapterRes{Seq: req.Seq, WordCount: wc, ChapterCount: count}, nil
}

const maxComicsZip = 2 << 30

func (c *Admin) ImportComics(ctx context.Context, req *v1.ImportComicsReq) (res *v1.ImportComicsRes, err error) {
	r := ghttp.RequestFromCtx(ctx)
	up := r.GetUploadFile("file")
	if up == nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "请上传 zip 文件")
	}
	if up.Size > maxComicsZip {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "压缩包不能超过 2GB")
	}
	if strings.ToLower(filepath.Ext(up.Filename)) != ".zip" {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "只支持 .zip")
	}
	dir, err := os.MkdirTemp("", "comics-import-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	saved, err := up.Save(dir)
	if err != nil {
		return nil, err
	}
	resume := req.Resume || r.Get("resume").Bool()
	return c.svc.ImportComics(ctx, filepath.Join(dir, saved), resume)
}

const maxNovelsZip = 200 << 20

func (c *Admin) ImportNovels(ctx context.Context, req *v1.ImportNovelsReq) (res *v1.ImportNovelsRes, err error) {
	r := ghttp.RequestFromCtx(ctx)
	up := r.GetUploadFile("file")
	if up == nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "请上传 zip 文件")
	}
	if up.Size > maxNovelsZip {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "压缩包不能超过 200MB")
	}
	if strings.ToLower(filepath.Ext(up.Filename)) != ".zip" {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "只支持 .zip")
	}
	dir, err := os.MkdirTemp("", "novels-import-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	saved, err := up.Save(dir)
	if err != nil {
		return nil, err
	}
	resume := req.Resume || r.Get("resume").Bool()
	return c.svc.ImportNovels(ctx, filepath.Join(dir, saved), resume)
}

func (c *Admin) UpdateAsset(ctx context.Context, req *v1.UpdateAssetReq) (res *v1.UpdateAssetRes, err error) {
	a, err := c.svc.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, gerror.NewCode(errcode.CodeNotFound, "资产不存在")
	}
	if consts.IsDramaSeries(a.Kind, a.ParentId) {
		if err := c.svc.UpdateDrama(ctx, req.Id, req.Title, req.Remark, req.Intro, req.UpdateStatus); err != nil {
			return nil, err
		}
	} else if consts.IsDramaEpisode(a.Kind, a.ParentId) {
		if err := c.svc.UpdateEpisode(ctx, req.Id, req.Title, req.Seq); err != nil {
			return nil, err
		}
	} else {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "仅短剧支持此编辑接口")
	}
	return &v1.UpdateAssetRes{Id: req.Id}, nil
}

func (c *Admin) CreateEpisode(ctx context.Context, req *v1.CreateEpisodeReq) (res *v1.CreateEpisodeRes, err error) {
	code, created, err := c.svc.CreateEpisode(ctx, req.Id, req.Seq, req.Title)
	if err != nil {
		return nil, err
	}
	return &v1.CreateEpisodeRes{Id: code, Created: created}, nil
}

func (c *Admin) DramaImportLimits(ctx context.Context, req *v1.DramaImportLimitsReq) (res *v1.DramaImportLimitsRes, err error) {
	lim := c.svc.ImportLimits(ctx)
	return &lim, nil
}

func (c *Admin) ImportDramas(ctx context.Context, req *v1.ImportDramasReq) (res *v1.ImportDramasRes, err error) {
	r := ghttp.RequestFromCtx(ctx)
	up := r.GetUploadFile("file")
	if up == nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "请上传 zip 文件")
	}
	lim := c.svc.ImportLimits(ctx)
	if cl := r.Request.ContentLength; cl > 0 && lim.MaxBodyBytes > 0 && cl > lim.MaxBodyBytes {
		return nil, gerror.NewCode(errcode.CodeBadRequest,
			"请求体 "+humanAdminBytes(cl)+" 超过上限 "+humanAdminBytes(lim.MaxBodyBytes)+"（含表单余量，压缩包上限 "+humanAdminBytes(lim.MaxZipBytes)+"）")
	}
	if up.Size > lim.MaxZipBytes {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "压缩包超过上限 "+humanAdminBytes(lim.MaxZipBytes))
	}
	if strings.ToLower(filepath.Ext(up.Filename)) != ".zip" {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "只支持 .zip")
	}
	dir, err := os.MkdirTemp("", "drama-import-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	saved, err := up.Save(dir)
	if err != nil {
		return nil, err
	}
	resume := req.Resume || r.Get("resume").Bool()
	plan := req.Plan
	if plan == "" {
		plan = r.Get("plan").String()
	}
	return c.svc.ImportDramas(ctx, filepath.Join(dir, saved), up.Filename, plan, resume)
}

func humanAdminBytes(n int64) string {
	if n >= 1024*1024*1024 {
		return fmt.Sprintf("%.2fGB", float64(n)/1024/1024/1024)
	}
	if n >= 1024*1024 {
		return fmt.Sprintf("%.1fMB", float64(n)/1024/1024)
	}
	return fmt.Sprintf("%dB", n)
}

func toAdminItem(a domain.Asset) v1.AssetItem {
	cover := playsign.WrapCover(a.Code, a.CoverUrl, "admin")
	play := a.PlayUrl
	if !consts.IsChapterKind(a.Kind) {
		play = playsign.Wrap(a.Code, a.PlayUrl, "admin")
	}
	return v1.AssetItem{
		Id: a.Code, Title: a.Title, CoverUrl: cover, Status: a.Status,
		TranscodeStatus: a.TranscodeStatus, PlayUrl: play,
		DurationSec: a.DurationSec, Kind: a.Kind, Category: a.Category,
		ChapterCount: a.ChapterCount, WordCount: a.WordCount,
		ParentId: a.ParentId, Seq: a.Seq, UpdateStatus: a.UpdateStatus, Intro: a.Intro,
		CreatedAt: a.CreatedAt,
	}
}

func wrapAdminComicPages(code string, chs []v1.ComicChapterItem) []v1.ComicChapterItem {
	for i := range chs {
		for j := range chs[i].Pages {
			if u := playsign.WrapPage(code, chs[i].Pages[j].Key, "admin"); u != "" {
				chs[i].Pages[j].Url = u
			}
		}
	}
	return chs
}
