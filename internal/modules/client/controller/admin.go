package controller

import (
	"context"

	"github.com/gogf/gf/v2/errors/gerror"

	v1 "github.com/JarvanDante/my_media/api/admin/client/v1"
	"github.com/JarvanDante/my_media/internal/dao"
	"github.com/JarvanDante/my_media/internal/shared/errcode"
)

type Admin struct {
	repo *dao.ClientRepo
}

func NewAdmin(repo *dao.ClientRepo) *Admin { return &Admin{repo: repo} }

func (c *Admin) List(ctx context.Context, req *v1.ListReq) (res *v1.ListRes, err error) {
	list, total, err := c.repo.List(ctx, req.Page, req.Size, req.Keyword)
	if err != nil {
		return nil, err
	}
	res = &v1.ListRes{Total: total, List: make([]v1.ClientItem, 0, len(list))}
	for _, x := range list {
		res.List = append(res.List, v1.ClientItem{
			Id: x.Id, AppKey: x.AppKey, SiteCode: x.SiteCode, Status: x.Status,
			Remark: x.Remark, CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt,
		})
	}
	return res, nil
}

func (c *Admin) Upsert(ctx context.Context, req *v1.UpsertReq) (res *v1.UpsertRes, err error) {
	if err := c.repo.Upsert(ctx, req.AppKey, req.AppSecret, req.SiteCode, req.Remark, req.Status); err != nil {
		return nil, err
	}
	st := req.Status
	if st != 0 && st != 1 {
		st = 1
	}
	return &v1.UpsertRes{AppKey: req.AppKey, SiteCode: req.SiteCode, Status: st}, nil
}

func (c *Admin) Disable(ctx context.Context, req *v1.DisableReq) (res *v1.DisableRes, err error) {
	cli, err := c.repo.GetByAppKey(ctx, req.AppKey)
	if err != nil {
		return nil, err
	}
	if cli == nil {
		return nil, gerror.NewCode(errcode.CodeNotFound, "调用方不存在")
	}
	if err := c.repo.Disable(ctx, req.AppKey); err != nil {
		return nil, err
	}
	return &v1.DisableRes{AppKey: req.AppKey, Status: 0}, nil
}
