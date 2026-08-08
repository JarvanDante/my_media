package controller

import (
	"context"

	"github.com/gogf/gf/v2/errors/gerror"

	v1 "github.com/JarvanDante/my_media/api/admin/asset/v1"
	"github.com/JarvanDante/my_media/internal/modules/asset/domain"
	"github.com/JarvanDante/my_media/internal/modules/asset/service"
	"github.com/JarvanDante/my_media/internal/shared/errcode"
)

type Admin struct {
	svc service.Asset
}

func NewAdmin(svc service.Asset) *Admin { return &Admin{svc: svc} }

func (c *Admin) List(ctx context.Context, req *v1.ListReq) (res *v1.ListRes, err error) {
	list, total, err := c.svc.List(ctx, domain.ListFilter{
		Page: req.Page, Size: req.Size, Keyword: req.Keyword, Status: req.Status,
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
	id, err := c.svc.Create(ctx, req.Title, req.CoverUrl, req.Remark)
	if err != nil {
		return nil, err
	}
	return &v1.CreateRes{Id: id}, nil
}

func (c *Admin) Detail(ctx context.Context, req *v1.DetailReq) (res *v1.DetailRes, err error) {
	a, err := c.svc.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, gerror.NewCode(errcode.CodeNotFound, "资产不存在")
	}
	return &v1.DetailRes{
		AssetItem:      toAdminItem(*a),
		SourceBucket:   a.SourceBucket,
		SourceKey:      a.SourceKey,
		PlayKey:        a.PlayKey,
		TranscodeJobId: a.TranscodeJobId,
		TranscodeError: a.TranscodeError,
		Remark:         a.Remark,
	}, nil
}

func (c *Admin) UploadURL(ctx context.Context, req *v1.UploadURLReq) (res *v1.UploadURLRes, err error) {
	return c.svc.PresignUpload(ctx, req.Id, req.Filename)
}

func (c *Admin) Transcode(ctx context.Context, req *v1.TranscodeReq) (res *v1.TranscodeRes, err error) {
	jobID, err := c.svc.TriggerTranscode(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return &v1.TranscodeRes{JobId: jobID}, nil
}

func toAdminItem(a domain.Asset) v1.AssetItem {
	return v1.AssetItem{
		Id: a.Id, Title: a.Title, CoverUrl: a.CoverUrl, Status: a.Status,
		TranscodeStatus: a.TranscodeStatus, PlayUrl: a.PlayUrl,
		DurationSec: a.DurationSec, CreatedAt: a.CreatedAt,
	}
}
