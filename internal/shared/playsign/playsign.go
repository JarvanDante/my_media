// Package playsign 生成指向 my_play 播放网关的签名播放地址。
// 未配置 play_gateway.base_url 时返回原始 play_url(平滑降级)。
// 签名 v2: HMAC-SHA256(secret, code|site|exp|d|ip), d=试看秒数(0=完整), ip=绑定IP(空=不绑)。
package playsign

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

type conf struct {
	base   string
	secret string
	ttl    int64
}

var (
	c    conf
	once sync.Once
)

func load() {
	ctx := context.Background()
	c.base = strings.TrimRight(g.Cfg().MustGet(ctx, "play_gateway.base_url", "").String(), "/")
	c.secret = g.Cfg().MustGet(ctx, "play_gateway.secret", "").String()
	c.ttl = g.Cfg().MustGet(ctx, "play_gateway.token_ttl_sec", 14400).Int64()
	if c.ttl <= 0 {
		c.ttl = 14400
	}
}

func sign(code, site string, exp int64, d int, ip string) string {
	mac := hmac.New(sha256.New, []byte(c.secret))
	_, _ = fmt.Fprintf(mac, "%s|%s|%d|%d|%s", code, site, exp, d, ip)
	return hex.EncodeToString(mac.Sum(nil))
}

func buildURL(code, site string, exp int64, d int, ip string) string {
	u := fmt.Sprintf("%s/hls/%s/index.m3u8?e=%d&s=%s&sig=%s",
		c.base, url.PathEscape(code), exp, url.QueryEscape(site), sign(code, site, exp, d, ip))
	if d > 0 {
		u += fmt.Sprintf("&d=%d", d)
	}
	if ip != "" {
		u += "&i=" + url.QueryEscape(ip)
	}
	return u
}

// Wrap 默认签发(全量播放, 不绑IP, 全局 TTL)。raw 为空或网关未配置时原样返回。
func Wrap(code, raw, site string) string {
	once.Do(load)
	if c.base == "" || c.secret == "" || raw == "" || code == "" {
		return raw
	}
	return buildURL(code, site, time.Now().Unix()+c.ttl, 0, "")
}

// Enabled 网关是否已配置。
func Enabled() bool {
	once.Do(load)
	return c.base != "" && c.secret != ""
}

// SignURL 定制签发(play-token 接口用): ttlSec<=0 用全局默认; previewSec>0 试看; ip 非空则绑定。
func SignURL(code, site string, ttlSec int64, previewSec int, ip string) (playURL string, expiresAt int64) {
	once.Do(load)
	if !Enabled() || code == "" {
		return "", 0
	}
	if ttlSec <= 0 {
		ttlSec = c.ttl
	}
	exp := time.Now().Unix() + ttlSec
	return buildURL(code, site, exp, previewSec, ip), exp
}
