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

func sign(code, site string, exp int64, d int, ip string, iat int64) string {
	mac := hmac.New(sha256.New, []byte(c.secret))
	_, _ = fmt.Fprintf(mac, "%s|%s|%d|%d|%s|%d", code, site, exp, d, ip, iat)
	return hex.EncodeToString(mac.Sum(nil))
}

// playlistName 从已存播放地址推断清单文件名(master.m3u8 多码率 / index.m3u8 旧单档),
// 空或异常时默认 master.m3u8。保证新老资产都指向各自真实清单。
func playlistName(raw string) string {
	if raw != "" {
		if i := strings.LastIndex(raw, "/"); i >= 0 {
			name := raw[i+1:]
			if j := strings.IndexAny(name, "?#"); j >= 0 {
				name = name[:j]
			}
			if strings.HasSuffix(name, ".m3u8") {
				return name
			}
		}
	}
	return "master.m3u8"
}

func buildURL(code, site, file string, exp int64, d int, ip string, iat int64) string {
	u := fmt.Sprintf("%s/hls/%s/%s?e=%d&s=%s&t=%d&sig=%s",
		c.base, url.PathEscape(code), file, exp, url.QueryEscape(site), iat, sign(code, site, exp, d, ip, iat))
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
	now := time.Now().Unix()
	return buildURL(code, site, playlistName(raw), now+c.ttl, 0, "", now)
}

// WrapCover 把转码封面(MinIO 私有桶直链)改成 my_play 签名地址，浏览器经网关 302 预签名拉取。
// 自定义外链封面原样返回；网关未配置时也不改写。
func WrapCover(code, raw, site string) string {
	once.Do(load)
	if raw == "" || code == "" {
		return raw
	}
	if c.base == "" || c.secret == "" {
		return raw
	}
	if !isHlsCover(raw, code) {
		return raw
	}
	now := time.Now().Unix()
	return buildURL(code, site, "cover.jpg", now+c.ttl, 0, "", now)
}

func isHlsCover(raw, code string) bool {
	return strings.Contains(raw, "/media/hls/"+code+"/") && strings.Contains(raw, "cover.jpg")
}

// Enabled 网关是否已配置。
func Enabled() bool {
	once.Do(load)
	return c.base != "" && c.secret != ""
}

// SignURL 定制签发(play-token 接口用): ttlSec<=0 用全局默认; previewSec>0 试看; ip 非空则绑定。
func SignURL(code, site string, ttlSec int64, previewSec int, ip, raw string) (playURL string, expiresAt int64) {
	once.Do(load)
	if !Enabled() || code == "" {
		return "", 0
	}
	if ttlSec <= 0 {
		ttlSec = c.ttl
	}
	now := time.Now().Unix()
	exp := now + ttlSec
	return buildURL(code, site, playlistName(raw), exp, previewSec, ip, now), exp
}
