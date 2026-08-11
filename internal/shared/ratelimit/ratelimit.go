// Package ratelimit 进程内滑动窗口限流(按 key 统计每分钟/每小时事件数)。
// 单实例部署够用; 若 my_media 横向扩容多副本, 应改为 Redis 计数(INCR+EXPIRE)以跨副本一致。
package ratelimit

import (
	"context"
	"sync"

	"github.com/gogf/gf/v2/frame/g"
)

type entry struct {
	times []int64 // 最近一小时内事件的 unix 秒, 升序
}

type Limiter struct {
	mu      sync.Mutex
	data    map[string]*entry
	perMin  int
	perHour int
	ops     int
}

var (
	inst *Limiter
	once sync.Once
)

// Default 懒加载单例, 阈值从配置读(open_ratelimit.per_min / per_hour)。
func Default(ctx context.Context) *Limiter {
	once.Do(func() {
		inst = &Limiter{
			data:    map[string]*entry{},
			perMin:  g.Cfg().MustGet(ctx, "open_ratelimit.per_min", 60).Int(),
			perHour: g.Cfg().MustGet(ctx, "open_ratelimit.per_hour", 600).Int(),
		}
	})
	return inst
}

// Allow 记录一次事件并判断是否放行。now 为当前 unix 秒(调用方传入, 便于测试)。
// 返回: 是否放行 / 建议重试秒数 / 命中原因。
func (l *Limiter) Allow(key string, now int64) (allowed bool, retryAfter int, reason string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	e := l.data[key]
	if e == nil {
		e = &entry{}
		l.data[key] = e
	}
	// 裁掉一小时前的记录
	cutoff := now - 3600
	i := 0
	for i < len(e.times) && e.times[i] < cutoff {
		i++
	}
	if i > 0 {
		e.times = e.times[i:]
	}
	// 统计
	minCutoff := now - 60
	minCnt := 0
	for _, t := range e.times {
		if t >= minCutoff {
			minCnt++
		}
	}
	hourCnt := len(e.times)

	if l.perMin > 0 && minCnt >= l.perMin {
		return false, 60, "请求过于频繁(每分钟上限)"
	}
	if l.perHour > 0 && hourCnt >= l.perHour {
		return false, 3600, "请求过于频繁(每小时上限)"
	}

	e.times = append(e.times, now)

	// 惰性清理: 每 2000 次调用扫一遍, 删掉空的/过期的 key, 防止 map 无限增长
	l.ops++
	if l.ops >= 2000 {
		l.ops = 0
		for k, v := range l.data {
			if len(v.times) == 0 || v.times[len(v.times)-1] < cutoff {
				delete(l.data, k)
			}
		}
	}
	return true, 0, ""
}
