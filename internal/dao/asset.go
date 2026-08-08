package dao

import (
	"context"
	"errors"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"

	"github.com/JarvanDante/my_media/internal/modules/asset/domain"
)

var ErrAssetNotReady = errors.New("asset not ready")

type assetRepo struct{}

func NewAssetRepo() domain.Repository { return &assetRepo{} }

func (r *assetRepo) List(ctx context.Context, f domain.ListFilter) ([]domain.Asset, int, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Size < 1 || f.Size > 100 {
		f.Size = 20
	}
	m := g.DB().Model("media_asset").Ctx(ctx).Safe()
	if f.Keyword != "" {
		m = m.WhereLike("title", "%"+f.Keyword+"%")
	}
	if f.ReadyOnly {
		m = m.Where("status", 2)
	} else if f.Status >= 0 {
		m = m.Where("status", f.Status)
	}
	total, err := m.Clone().Count()
	if err != nil {
		return nil, 0, err
	}
	rows, err := m.OrderDesc("id").Page(f.Page, f.Size).All()
	if err != nil {
		return nil, 0, err
	}
	list := make([]domain.Asset, 0, len(rows))
	for _, row := range rows {
		list = append(list, mapAsset(row.Map()))
	}
	return list, total, nil
}

func (r *assetRepo) Create(ctx context.Context, title, coverUrl, remark string) (int64, error) {
	return g.DB().Model("media_asset").Ctx(ctx).Data(g.Map{
		"title":            title,
		"cover_url":        coverUrl,
		"remark":           remark,
		"status":           0,
		"transcode_status": "none",
		"created_at":       gtime.Now(),
		"updated_at":       gtime.Now(),
	}).InsertAndGetId()
}

func (r *assetRepo) Get(ctx context.Context, id int64) (*domain.Asset, error) {
	row, err := g.DB().Model("media_asset").Ctx(ctx).Where("id", id).One()
	if err != nil {
		return nil, err
	}
	if row.IsEmpty() {
		return nil, nil
	}
	a := mapAsset(row.Map())
	return &a, nil
}

func (r *assetRepo) Pick(ctx context.Context, appKey, siteCode string, assetID int64) (*domain.Asset, error) {
	a, err := r.Get(ctx, assetID)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, nil
	}
	if a.Status != 2 {
		return nil, ErrAssetNotReady
	}
	exist, err := g.DB().Model("site_asset_pick").Ctx(ctx).
		Where("app_key", appKey).Where("asset_id", assetID).One()
	if err != nil {
		return nil, err
	}
	if exist.IsEmpty() {
		_, err = g.DB().Model("site_asset_pick").Ctx(ctx).Data(g.Map{
			"app_key":    appKey,
			"site_code":  siteCode,
			"asset_id":   assetID,
			"created_at": gtime.Now(),
		}).Insert()
		if err != nil {
			return nil, err
		}
	}
	return a, nil
}

func mapAsset(m g.Map) domain.Asset {
	return domain.Asset{
		Id:              g.NewVar(m["id"]).Int64(),
		Title:           g.NewVar(m["title"]).String(),
		CoverUrl:        g.NewVar(m["cover_url"]).String(),
		SourceBucket:    g.NewVar(m["source_bucket"]).String(),
		SourceKey:       g.NewVar(m["source_key"]).String(),
		PlayKey:         g.NewVar(m["play_key"]).String(),
		PlayUrl:         g.NewVar(m["play_url"]).String(),
		DurationSec:     g.NewVar(m["duration_sec"]).Int(),
		Status:          g.NewVar(m["status"]).Int(),
		TranscodeStatus: g.NewVar(m["transcode_status"]).String(),
		TranscodeJobId:  g.NewVar(m["transcode_job_id"]).String(),
		TranscodeError:  g.NewVar(m["transcode_error"]).String(),
		Remark:          g.NewVar(m["remark"]).String(),
		CreatedAt:       g.NewVar(m["created_at"]).String(),
	}
}
