// Package play 播放服务模块: 策略管理 / 统计查询 / 网关同步。
package play

import (
	"context"
	"time"

	v1 "github.com/JarvanDante/my_media/api/play/v1"
	"github.com/JarvanDante/my_media/internal/dao"
	"github.com/JarvanDante/my_media/internal/model/entity"
)

type Controller struct {
	repo *dao.PlayRepo
}

func New(repo *dao.PlayRepo) *Controller { return &Controller{repo: repo} }

func toItem(p *entity.PlayPolicy) v1.PolicyItem {
	updated := ""
	if p.UpdatedAt != nil {
		updated = p.UpdatedAt.Format("Y-m-d H:i:s")
	}
	return v1.PolicyItem{
		SiteCode: p.SiteCode, RefererWhitelist: p.RefererWhitelist,
		UaBlacklist: p.UaBlacklist, TokenTtlSec: p.TokenTtlSec,
		Status: p.Status, UpdatedAt: updated,
	}
}

func (c *Controller) PolicyList(ctx context.Context, req *v1.PolicyListReq) (res *v1.PolicyListRes, err error) {
	list, err := c.repo.PolicyList(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]v1.PolicyItem, 0, len(list))
	for _, p := range list {
		out = append(out, toItem(p))
	}
	return &v1.PolicyListRes{List: out}, nil
}

func (c *Controller) PolicyUpsert(ctx context.Context, req *v1.PolicyUpsertReq) (res *v1.PolicyUpsertRes, err error) {
	if err = c.repo.PolicyUpsert(ctx, &entity.PlayPolicy{
		SiteCode: req.SiteCode, RefererWhitelist: req.RefererWhitelist,
		UaBlacklist: req.UaBlacklist, TokenTtlSec: req.TokenTtlSec, Status: req.Status,
	}); err != nil {
		return nil, err
	}
	return &v1.PolicyUpsertRes{}, nil
}

func (c *Controller) Stats(ctx context.Context, req *v1.StatsReq) (res *v1.StatsRes, err error) {
	list, err := c.repo.StatsQuery(ctx, req.Start, req.End, req.SiteCode, 500)
	if err != nil {
		return nil, err
	}
	out := make([]v1.StatItem, 0, len(list))
	for _, s := range list {
		day := ""
		if s.Day != nil {
			day = s.Day.Format("Y-m-d")
		}
		out = append(out, v1.StatItem{
			Day: day, SiteCode: s.SiteCode, AssetCode: s.AssetCode,
			Plays: s.Plays, SegReqs: s.SegReqs,
		})
	}
	return &v1.StatsRes{List: out}, nil
}

// ---- 网关内部 ----

func (c *Controller) GwPolicies(ctx context.Context, req *v1.GwPoliciesReq) (res *v1.GwPoliciesRes, err error) {
	list, err := c.repo.PolicyList(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]v1.PolicyItem, 0, len(list))
	for _, p := range list {
		out = append(out, toItem(p))
	}
	return &v1.GwPoliciesRes{List: out}, nil
}

func (c *Controller) GwStatsIngest(ctx context.Context, req *v1.GwStatsIngestReq) (res *v1.GwStatsIngestRes, err error) {
	day := time.Now().Format("2006-01-02")
	n := 0
	for _, it := range req.Items {
		if it.SiteCode == "" || it.AssetCode == "" || (it.Plays <= 0 && it.SegReqs <= 0) {
			continue
		}
		if err := c.repo.StatsIncr(ctx, day, it.SiteCode, it.AssetCode, it.Plays, it.SegReqs); err != nil {
			return nil, err
		}
		n++
	}
	return &v1.GwStatsIngestRes{Accepted: n}, nil
}
