// Package playsign 生成指向 my_play 播放网关的签名播放地址。
// 未配置 play_gateway.base_url 时返回原始 play_url(平滑降级)。
// 签名 v4: HMAC-SHA256(secret, code|site|exp|d|ip|iat|p), d=试看秒数(0=完整), ip=绑定IP(空=不绑), p=用途(img/hls)。
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

func sign(code, site string, exp int64, d int, ip string, iat int64, p string) string {
	mac := hmac.New(sha256.New, []byte(c.secret))
	_, _ = fmt.Fprintf(mac, "%s|%s|%d|%d|%s|%d|%s", code, site, exp, d, ip, iat, p)
	return hex.EncodeToString(mac.Sum(nil))
}

// purpose 清单签 hls，其余(封面/页图)签 img，my_play 按请求文件类型校验。
func purpose(file string) string {
	if strings.HasSuffix(strings.ToLower(file), ".m3u8") {
		return "hls"
	}
	return "img"
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

// PlaylistName 导出清单文件名推断，供开放接口返回各集清单名。
func PlaylistName(raw string) string { return playlistName(raw) }

func buildURL(code, site, file string, exp int64, d int, ip string, iat int64) string {
	p := purpose(file)
	u := fmt.Sprintf("%s/hls/%s/%s?e=%d&s=%s&t=%d&p=%s&sig=%s",
		c.base, url.PathEscape(code), file, exp, url.QueryEscape(site), iat, p, sign(code, site, exp, d, ip, iat, p))
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
	if code == "" {
		return raw
	}
	if c.base == "" || c.secret == "" {
		return raw
	}
	if raw != "" && !isPrivateMediaCover(raw, code) {
		return raw
	}
	now := time.Now().Unix()
	file := "cover.bnc"
	if site == "admin" {
		// 后台 <img> 不解密, 走网关 cover.jpg 明文预览(同 token 即可)。
		file = "cover.jpg"
	}
	return buildURL(code, site, file, now+c.ttl, 0, "", now)
}

// WrapPage 漫画页图签名。objectKey 形如 comics/{code}/ch001/page_001.bnc。
// admin → .jpg 明文预览；其它站点 → .bnc 密文给 H5。网关未配置时返回空串由调用方回退。
func WrapPage(code, objectKey, site string) string {
	once.Do(load)
	if !Enabled() || code == "" {
		return ""
	}
	rel := comicPageRel(code, objectKey)
	if rel == "" {
		return ""
	}
	if site == "admin" {
		rel = withImageExt(rel, ".jpg")
	} else {
		rel = withImageExt(rel, ".bnc")
	}
	now := time.Now().Unix()
	return buildURL(code, site, rel, now+c.ttl, 0, "", now)
}

func comicPageRel(code, key string) string {
	key = strings.TrimLeft(strings.TrimSpace(key), "/")
	if i := strings.Index(key, "?"); i >= 0 {
		key = key[:i]
	}
	if code == "" || key == "" {
		return ""
	}
	prefix := "comics/" + code + "/"
	if !strings.HasPrefix(key, prefix) {
		if j := strings.Index(key, "/"+prefix); j >= 0 {
			key = key[j+1:]
		}
	}
	if !strings.HasPrefix(key, prefix) {
		return ""
	}
	rel := key[len(prefix):]
	if rel == "" || strings.Contains(rel, "..") {
		return ""
	}
	if strings.Count(rel, "/") > 1 {
		return ""
	}
	return rel
}

func withImageExt(rel, ext string) string {
	if i := strings.LastIndex(rel, "."); i >= 0 {
		return rel[:i] + ext
	}
	return rel + ext
}

func isHlsCover(raw, code string) bool {
	if !strings.Contains(raw, "cover.jpg") && !strings.Contains(raw, "cover.bnc") {
		return false
	}
	return strings.Contains(raw, "/media/hls/"+code+"/") ||
		strings.Contains(raw, "/cartoon/hls/"+code+"/") ||
		strings.Contains(raw, "/video/hls/"+code+"/") ||
		strings.Contains(raw, "/drama/hls/"+code+"/")
}

func isPrivateMediaCover(raw, code string) bool {
	if isHlsCover(raw, code) {
		return true
	}
	if strings.Contains(raw, "host.docker.internal") || strings.Contains(raw, ":19000/") {
		return true
	}
	return strings.Contains(raw, "/comics/"+code+"/") ||
		strings.Contains(raw, "/novels/"+code+"/") ||
		strings.Contains(raw, "/drama/"+code+"/") ||
		strings.Contains(raw, "/my-media/comics/") ||
		strings.Contains(raw, "/my-media/novels/") ||
		strings.Contains(raw, "/my-media/cartoon/") ||
		strings.Contains(raw, "/my-media/video/") ||
		strings.Contains(raw, "/my-media/drama/")
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
