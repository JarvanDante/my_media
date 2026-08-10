// Package playsign 生成指向 my_play 播放网关的签名播放地址。
// 未配置 play_gateway.base_url 时返回原始 play_url(平滑降级)。
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

// sign 与 my_play 的 token 包一致: HMAC-SHA256(code|site|exp)。
func sign(code, site string, exp int64) string {
	mac := hmac.New(sha256.New, []byte(c.secret))
	_, _ = fmt.Fprintf(mac, "%s|%s|%d", code, site, exp)
	return hex.EncodeToString(mac.Sum(nil))
}

// Wrap 把资产的播放地址替换为网关签名地址。
// raw 为空(未转码完成)或网关未配置时原样返回。
func Wrap(code, raw, site string) string {
	once.Do(load)
	if c.base == "" || c.secret == "" || raw == "" || code == "" {
		return raw
	}
	exp := time.Now().Unix() + c.ttl
	return fmt.Sprintf("%s/hls/%s/index.m3u8?e=%d&s=%s&sig=%s",
		c.base, url.PathEscape(code), exp, url.QueryEscape(site), sign(code, site, exp))
}
