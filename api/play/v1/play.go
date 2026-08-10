// Package v1 播放服务(M2)接口契约: 管理端策略/统计 + 网关内部同步。
package v1

import "github.com/gogf/gf/v2/frame/g"

type PolicyItem struct {
	SiteCode         string `json:"site_code"`
	RefererWhitelist string `json:"referer_whitelist"`
	UaBlacklist      string `json:"ua_blacklist"`
	TokenTtlSec      int    `json:"token_ttl_sec"`
	Status           int    `json:"status"`
	UpdatedAt        string `json:"updated_at"`
}

// ---- 管理端(X-Admin-Token) ----

type PolicyListReq struct {
	g.Meta `path:"/admin/play/policies" method:"get" tags:"Admin/Play" summary:"播放策略列表"`
}
type PolicyListRes struct {
	List []PolicyItem `json:"list"`
}

type PolicyUpsertReq struct {
	g.Meta           `path:"/admin/play/policies/{site_code}" method:"put" tags:"Admin/Play" summary:"保存站点播放策略"`
	SiteCode         string `json:"site_code" in:"path" v:"required"`
	RefererWhitelist string `json:"referer_whitelist"`
	UaBlacklist      string `json:"ua_blacklist"`
	TokenTtlSec      int    `json:"token_ttl_sec" v:"min:60#有效期至少60秒"`
	Status           int    `json:"status" v:"in:0,1"`
}
type PolicyUpsertRes struct{}

type StatItem struct {
	Day       string `json:"day"`
	SiteCode  string `json:"site_code"`
	AssetCode string `json:"asset_code"`
	Plays     int64  `json:"plays"`
	SegReqs   int64  `json:"seg_reqs"`
}

type StatsReq struct {
	g.Meta   `path:"/admin/play/stats" method:"get" tags:"Admin/Play" summary:"播放统计查询"`
	Start    string `json:"start" v:"required#开始日期必填"` // YYYY-MM-DD
	End      string `json:"end"   v:"required#结束日期必填"`
	SiteCode string `json:"site_code"`
}
type StatsRes struct {
	List []StatItem `json:"list"`
}

// ---- 网关内部(X-Play-Token) ----

type GwPoliciesReq struct {
	g.Meta `path:"/gw/play/policies" method:"get" tags:"Gw/Play" summary:"网关拉取全部策略"`
}
type GwPoliciesRes struct {
	List []PolicyItem `json:"list"`
}

type GwStatItem struct {
	SiteCode  string `json:"site_code"`
	AssetCode string `json:"asset_code"`
	Plays     int64  `json:"plays"`
	SegReqs   int64  `json:"seg_reqs"`
}

type GwStatsIngestReq struct {
	g.Meta `path:"/gw/play/stats" method:"post" tags:"Gw/Play" summary:"网关批量上报统计"`
	Items  []GwStatItem `json:"items"`
}
type GwStatsIngestRes struct {
	Accepted int `json:"accepted"`
}

// ---- M3-2 链接失效闸 ----

type RevokeItem struct {
	SiteCode  string `json:"site_code"`
	AssetCode string `json:"asset_code"`
	NotBefore int64  `json:"not_before"`
	UpdatedAt string `json:"updated_at"`
}

// 管理端(X-Admin-Token)
type RevokeListReq struct {
	g.Meta `path:"/admin/play/revokes" method:"get" tags:"Admin/Play" summary:"链接失效闸列表"`
}
type RevokeListRes struct {
	List []RevokeItem `json:"list"`
}

type RevokeReq struct {
	g.Meta    `path:"/admin/play/revoke" method:"post" tags:"Admin/Play" summary:"一键失效(站点或指定资产的现有链接)"`
	SiteCode  string `json:"site_code" v:"required#站点必填"`
	AssetCode string `json:"asset_code"` // 空=整站
}
type RevokeRes struct {
	NotBefore int64 `json:"not_before"`
}

// 网关内部(X-Play-Token)
type GwRevokesReq struct {
	g.Meta `path:"/gw/play/revokes" method:"get" tags:"Gw/Play" summary:"网关拉取失效闸"`
}
type GwRevokesRes struct {
	List []RevokeItem `json:"list"`
}
