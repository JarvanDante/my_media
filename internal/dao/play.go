// Package dao 播放策略/统计存取。
package dao

import (
	"context"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"

	"github.com/JarvanDante/my_media/internal/model/entity"
)

type PlayRepo struct{}

func NewPlayRepo() *PlayRepo { return &PlayRepo{} }

func (r *PlayRepo) PolicyList(ctx context.Context) ([]*entity.PlayPolicy, error) {
	var list []*entity.PlayPolicy
	err := g.Model("play_policy").Ctx(ctx).Order("site_code asc").Scan(&list)
	return list, err
}

func (r *PlayRepo) PolicyUpsert(ctx context.Context, p *entity.PlayPolicy) error {
	_, err := g.Model("play_policy").Ctx(ctx).Data(g.Map{
		"site_code": p.SiteCode, "referer_whitelist": p.RefererWhitelist,
		"ua_blacklist": p.UaBlacklist, "token_ttl_sec": p.TokenTtlSec,
		"status": p.Status, "updated_at": gtime.Now(),
	}).OnConflict("site_code").Save()
	return err
}

func (r *PlayRepo) PolicyGet(ctx context.Context, siteCode string) (*entity.PlayPolicy, error) {
	var p *entity.PlayPolicy
	err := g.Model("play_policy").Ctx(ctx).Where("site_code", siteCode).Scan(&p)
	return p, err
}

// StatsIncr 按日累加(upsert)。
func (r *PlayRepo) StatsIncr(ctx context.Context, day string, siteCode, assetCode string, plays, segReqs int64) error {
	_, err := g.DB().Exec(ctx, `
INSERT INTO play_stat_daily (day, site_code, asset_code, plays, seg_reqs)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (day, site_code, asset_code)
DO UPDATE SET plays = play_stat_daily.plays + EXCLUDED.plays,
              seg_reqs = play_stat_daily.seg_reqs + EXCLUDED.seg_reqs`,
		day, siteCode, assetCode, plays, segReqs)
	return err
}

func (r *PlayRepo) StatsQuery(ctx context.Context, start, end, siteCode string, limit int) ([]*entity.PlayStatDaily, error) {
	m := g.Model("play_stat_daily").Ctx(ctx).
		WhereGTE("day", start).WhereLTE("day", end)
	if siteCode != "" {
		m = m.Where("site_code", siteCode)
	}
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	var list []*entity.PlayStatDaily
	err := m.Order("day desc, plays desc").Limit(limit).Scan(&list)
	return list, err
}
