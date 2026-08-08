package dao

import (
	"context"
	"errors"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"

	"github.com/JarvanDante/my_media/internal/consts"
	"github.com/JarvanDante/my_media/internal/modules/asset/domain"
	"github.com/JarvanDante/my_media/internal/shared/transcode"
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
		m = m.Where("status", consts.AssetStatusReady)
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
		"status":           consts.AssetStatusDraft,
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

func (r *assetRepo) ListPicks(ctx context.Context, appKey string, page, size int) ([]domain.PickRecord, int, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	m := g.DB().Model("site_asset_pick p").Ctx(ctx).
		LeftJoin("media_asset a", "a.id=p.asset_id").
		Where("p.app_key", appKey).
		Where("a.status", consts.AssetStatusReady)
	total, err := m.Clone().Count()
	if err != nil {
		return nil, 0, err
	}
	rows, err := m.Fields("p.asset_id, p.created_at as picked_at, a.title, a.cover_url, a.play_url, a.play_key, a.duration_sec").
		OrderDesc("p.id").Page(page, size).All()
	if err != nil {
		return nil, 0, err
	}
	list := make([]domain.PickRecord, 0, len(rows))
	for _, row := range rows {
		list = append(list, domain.PickRecord{
			AssetId:     row["asset_id"].Int64(),
			Title:       row["title"].String(),
			CoverUrl:    row["cover_url"].String(),
			PlayUrl:     row["play_url"].String(),
			PlayKey:     row["play_key"].String(),
			DurationSec: row["duration_sec"].Int(),
			PickedAt:    row["picked_at"].String(),
		})
	}
	return list, total, nil
}

func (r *assetRepo) PickedSet(ctx context.Context, appKey string, assetIDs []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(assetIDs) == 0 {
		return out, nil
	}
	rows, err := g.DB().Model("site_asset_pick").Ctx(ctx).
		Where("app_key", appKey).WhereIn("asset_id", assetIDs).
		Fields("asset_id").All()
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row["asset_id"].Int64()] = true
	}
	return out, nil
}

func (r *assetRepo) Pick(ctx context.Context, appKey, siteCode string, assetID int64) (*domain.Asset, error) {
	a, err := r.Get(ctx, assetID)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, nil
	}
	if a.Status != consts.AssetStatusReady {
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

func (r *assetRepo) BindSource(ctx context.Context, id int64, bucket, key string) error {
	_, err := g.DB().Model("media_asset").Ctx(ctx).Where("id", id).Data(g.Map{
		"source_bucket": bucket,
		"source_key":    key,
		"updated_at":    gtime.Now(),
	}).Update()
	return err
}

func (r *assetRepo) MarkTranscoding(ctx context.Context, id int64, jobID, profile string) error {
	return g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		_, err := tx.Model("media_asset").Ctx(ctx).Where("id", id).Data(g.Map{
			"status":           consts.AssetStatusProcessing,
			"transcode_status": "pending",
			"transcode_job_id": jobID,
			"transcode_error":  "",
			"updated_at":       gtime.Now(),
		}).Update()
		if err != nil {
			return err
		}
		_, err = tx.Model("transcode_job").Ctx(ctx).Data(g.Map{
			"job_id":     jobID,
			"asset_id":   id,
			"profile":    profile,
			"status":     "pending",
			"created_at": gtime.Now(),
		}).Insert()
		return err
	})
}

func (r *assetRepo) ApplyTranscodeResult(ctx context.Context, res domain.TranscodeResult) error {
	if res.JobID == "" {
		return nil
	}
	job, err := g.DB().Model("transcode_job").Ctx(ctx).Where("job_id", res.JobID).One()
	if err != nil {
		return err
	}
	var assetID int64
	if !job.IsEmpty() {
		assetID = job["asset_id"].Int64()
		jobData := g.Map{
			"status": res.Status,
			"error":  res.Error,
		}
		if res.PlayKey != "" {
			jobData["play_key"] = res.PlayKey
		}
		if res.PlayURL != "" {
			jobData["play_url"] = res.PlayURL
		}
		if res.Status == transcode.StatusReady || res.Status == transcode.StatusFailed {
			jobData["finished_at"] = gtime.Now()
		}
		if _, err := g.DB().Model("transcode_job").Ctx(ctx).Where("job_id", res.JobID).Data(jobData).Update(); err != nil {
			return err
		}
	} else {
		asset, err := g.DB().Model("media_asset").Ctx(ctx).Where("transcode_job_id", res.JobID).One()
		if err != nil {
			return err
		}
		if asset.IsEmpty() {
			return nil
		}
		assetID = asset["id"].Int64()
	}
	return r.applyToAsset(ctx, assetID, res)
}

func (r *assetRepo) applyToAsset(ctx context.Context, assetID int64, res domain.TranscodeResult) error {
	data := g.Map{
		"transcode_status": res.Status,
		"transcode_error":  res.Error,
		"updated_at":       gtime.Now(),
	}
	switch res.Status {
	case transcode.StatusProcessing:
		data["status"] = consts.AssetStatusProcessing
		data["transcode_status"] = "processing"
	case transcode.StatusReady:
		data["status"] = consts.AssetStatusReady
		data["play_key"] = res.PlayKey
		data["play_url"] = res.PlayURL
		if res.DurationSec > 0 {
			data["duration_sec"] = res.DurationSec
		}
		data["transcode_error"] = ""
	case transcode.StatusFailed:
		data["status"] = consts.AssetStatusFailed
	}
	_, err := g.DB().Model("media_asset").Ctx(ctx).Where("id", assetID).Data(data).Update()
	return err
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
