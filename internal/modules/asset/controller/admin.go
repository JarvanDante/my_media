package controller

import (
	"context"
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
	code, err := c.svc.Create(ctx, req.Title, req.CoverUrl, req.Remark)
	if err != nil {
		return nil, err
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
		Intro:          a.Intro,
	}
	if a.Kind == consts.KindComics {
		chs, e := c.svc.ComicChapters(ctx, a.Code)
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

const maxComicsZip = 2 << 30

func (c *Admin) ImportComics(ctx context.Context, _ *v1.ImportComicsReq) (res *v1.ImportComicsRes, err error) {
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
	return c.svc.ImportComics(ctx, filepath.Join(dir, saved))
}

func toAdminItem(a domain.Asset) v1.AssetItem {
	cover := playsign.WrapCover(a.Code, a.CoverUrl, "admin")
	play := a.PlayUrl
	if a.Kind != consts.KindComics {
		play = playsign.Wrap(a.Code, a.PlayUrl, "admin")
	}
	return v1.AssetItem{
		Id: a.Code, Title: a.Title, CoverUrl: cover, Status: a.Status,
		TranscodeStatus: a.TranscodeStatus, PlayUrl: play,
		DurationSec: a.DurationSec, Kind: a.Kind, Category: a.Category,
		ChapterCount: a.ChapterCount, CreatedAt: a.CreatedAt,
	}
}
