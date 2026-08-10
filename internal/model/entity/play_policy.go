// Code maintained manually (播放策略/统计).
package entity

import "github.com/gogf/gf/v2/os/gtime"

type PlayPolicy struct {
	SiteCode         string      `json:"siteCode"         orm:"site_code"`
	RefererWhitelist string      `json:"refererWhitelist" orm:"referer_whitelist"`
	UaBlacklist      string      `json:"uaBlacklist"      orm:"ua_blacklist"`
	TokenTtlSec      int         `json:"tokenTtlSec"      orm:"token_ttl_sec"`
	Status           int         `json:"status"           orm:"status"`
	UpdatedAt        *gtime.Time `json:"updatedAt"        orm:"updated_at"`
}

type PlayStatDaily struct {
	Day       *gtime.Time `json:"day"       orm:"day"`
	SiteCode  string      `json:"siteCode"  orm:"site_code"`
	AssetCode string      `json:"assetCode" orm:"asset_code"`
	Plays     int64       `json:"plays"     orm:"plays"`
	SegReqs   int64       `json:"segReqs"   orm:"seg_reqs"`
}

type PlayRevoke struct {
	SiteCode  string      `json:"siteCode"  orm:"site_code"`
	AssetCode string      `json:"assetCode" orm:"asset_code"`
	NotBefore int64       `json:"notBefore" orm:"not_before"`
	UpdatedAt *gtime.Time `json:"updatedAt" orm:"updated_at"`
}
