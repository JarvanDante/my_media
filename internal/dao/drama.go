package dao

import (
	"context"
	"errors"
	"strings"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"

	"github.com/JarvanDante/my_media/internal/consts"
	"github.com/JarvanDante/my_media/internal/modules/asset/domain"
	"github.com/JarvanDante/my_media/internal/shared/kit"
)

func (r *assetRepo) CreateDrama(ctx context.Context, title, coverUrl, remark, intro string, updateStatus int) (string, error) {
	if updateStatus != consts.UpdateStatusEnded {
		updateStatus = consts.UpdateStatusSerial
	}
	for i := 0; i < 8; i++ {
		c, err := kit.NewPublicID()
		if err != nil {
			return "", err
		}
		exist, err := g.DB().Model("media_asset").Ctx(ctx).Where("code", c).One()
		if err != nil {
			return "", err
		}
		if !exist.IsEmpty() {
			continue
		}
		_, err = g.DB().Model("media_asset").Ctx(ctx).Data(g.Map{
			"code":             c,
			"title":            title,
			"cover_url":        coverUrl,
			"remark":           remark,
			"intro":            intro,
			"kind":             consts.KindDrama,
			"parent_id":        0,
			"seq":              0,
			"update_status":    updateStatus,
			"status":           consts.AssetStatusDraft,
			"transcode_status": "none",
			"created_at":       gtime.Now(),
			"updated_at":       gtime.Now(),
		}).Insert()
		if err != nil {
			return "", err
		}
		return c, nil
	}
	return "", errors.New("生成资产短码失败, 请重试")
}

func (r *assetRepo) UpdateDrama(ctx context.Context, pk int64, title, remark, intro string, updateStatus int) error {
	if updateStatus != consts.UpdateStatusEnded && updateStatus != consts.UpdateStatusSerial {
		updateStatus = consts.UpdateStatusSerial
	}
	_, err := g.DB().Model("media_asset").Ctx(ctx).Where("id", pk).Data(g.Map{
		"title":          title,
		"remark":         remark,
		"intro":          intro,
		"update_status":  updateStatus,
		"updated_at":     gtime.Now(),
	}).Update()
	return err
}

func (r *assetRepo) CreateEpisode(ctx context.Context, parentPK int64, seq int, title string) (string, bool, error) {
	if parentPK <= 0 || seq < 1 {
		return "", false, errors.New("无效的整部或集号")
	}
	if old, err := r.FindEpisode(ctx, parentPK, seq); err != nil {
		return "", false, err
	} else if old != nil {
		return old.Code, false, nil
	}
	for i := 0; i < 8; i++ {
		c, err := kit.NewPublicID()
		if err != nil {
			return "", false, err
		}
		exist, err := g.DB().Model("media_asset").Ctx(ctx).Where("code", c).One()
		if err != nil {
			return "", false, err
		}
		if !exist.IsEmpty() {
			continue
		}
		_, err = g.DB().Model("media_asset").Ctx(ctx).Data(g.Map{
			"code":             c,
			"title":            title,
			"kind":             consts.KindDrama,
			"parent_id":        parentPK,
			"seq":              seq,
			"status":           consts.AssetStatusDraft,
			"transcode_status": "none",
			"created_at":       gtime.Now(),
			"updated_at":       gtime.Now(),
		}).Insert()
		if err != nil {
			if isUniqueViolation(err) {
				if old, e := r.FindEpisode(ctx, parentPK, seq); e == nil && old != nil {
					return old.Code, false, nil
				}
			}
			return "", false, err
		}
		return c, true, nil
	}
	return "", false, errors.New("生成资产短码失败, 请重试")
}

func (r *assetRepo) UpdateEpisodeMeta(ctx context.Context, pk int64, title string, seq int) error {
	_, err := g.DB().Model("media_asset").Ctx(ctx).Where("id", pk).Data(g.Map{
		"title":      title,
		"seq":        seq,
		"updated_at": gtime.Now(),
	}).Update()
	return err
}

func (r *assetRepo) FindDramasByTitle(ctx context.Context, title string) ([]domain.Asset, error) {
	rows, err := g.DB().Model("media_asset").Ctx(ctx).
		Where("kind", consts.KindDrama).Where("parent_id", 0).Where("title", title).
		OrderAsc("id").All()
	if err != nil {
		return nil, err
	}
	list := make([]domain.Asset, 0, len(rows))
	for _, row := range rows {
		list = append(list, mapAsset(row.Map()))
	}
	return list, nil
}

func (r *assetRepo) ListEpisodes(ctx context.Context, parentPK int64) ([]domain.Asset, error) {
	rows, err := g.DB().Model("media_asset").Ctx(ctx).
		Where("parent_id", parentPK).OrderAsc("seq").OrderAsc("id").All()
	if err != nil {
		return nil, err
	}
	list := make([]domain.Asset, 0, len(rows))
	for _, row := range rows {
		list = append(list, mapAsset(row.Map()))
	}
	return list, nil
}

func (r *assetRepo) FindEpisode(ctx context.Context, parentPK int64, seq int) (*domain.Asset, error) {
	row, err := g.DB().Model("media_asset").Ctx(ctx).
		Where("parent_id", parentPK).Where("seq", seq).One()
	if err != nil {
		return nil, err
	}
	if row.IsEmpty() {
		return nil, nil
	}
	a := mapAsset(row.Map())
	return &a, nil
}

func (r *assetRepo) UpdateChapterCount(ctx context.Context, pk int64, n int) error {
	_, err := g.DB().Model("media_asset").Ctx(ctx).Where("id", pk).Data(g.Map{
		"chapter_count": n,
		"updated_at":    gtime.Now(),
	}).Update()
	return err
}

func (r *assetRepo) DeleteTree(ctx context.Context, parentPK int64) error {
	if parentPK <= 0 {
		return errors.New("invalid asset pk")
	}
	return g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		rows, err := tx.Model("media_asset").Ctx(ctx).Where("parent_id", parentPK).Fields("id").All()
		if err != nil {
			return err
		}
		ids := []int64{parentPK}
		for _, row := range rows {
			ids = append(ids, row["id"].Int64())
		}
		if _, err := tx.Model("media_comic_chapter").Ctx(ctx).WhereIn("asset_id", ids).Delete(); err != nil {
			return err
		}
		if _, err := tx.Model("media_novel_chapter").Ctx(ctx).WhereIn("asset_id", ids).Delete(); err != nil {
			return err
		}
		if _, err := tx.Model("site_asset_pick").Ctx(ctx).WhereIn("asset_id", ids).Delete(); err != nil {
			return err
		}
		if _, err := tx.Model("transcode_job").Ctx(ctx).WhereIn("asset_id", ids).Delete(); err != nil {
			return err
		}
		_, err = tx.Model("media_asset").Ctx(ctx).WhereIn("id", ids).Delete()
		return err
	})
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "duplicate") || strings.Contains(s, "unique")
}
