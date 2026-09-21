package typesafe

import (
	"context"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RetryPolicy 控制可重试失败(限流、超时、瞬时服务端错误)的重试行为。
// 零值不可直接使用,请从 DefaultRetryPolicy 出发修改。
type RetryPolicy struct {
	// MaxRetries 是首次尝试之后的最大重试次数,0 表示禁用重试。
	MaxRetries int
	// BackoffInitial 是首次重试前的等待时长,每次重试翻倍,封顶 BackoffMax。
	BackoffInitial time.Duration
	// BackoffMax 是退避时长的上限。
	BackoffMax time.Duration
	// BackoffJitter 是每次等待随机缩短的比例(0~1),避免重试风暴。
	BackoffJitter float64
	// HTTPStatuses 自定义可重试的状态码集合;nil 表示默认的 408、429 与所有 5xx。
	HTTPStatuses map[int]bool
	// RetryConnectionErrors 是否重试网络连接错误。
	RetryConnectionErrors bool
	// RetryTimeouts 是否重试单次尝试超时。
	RetryTimeouts bool
	// RespectRetryAfter 是否尊重 Retry-After / retry-after-ms 响应头。
	RespectRetryAfter bool
	// MaxRetryAfter 是服务器指示等待时长的上限,超过则回退到本地退避。
	MaxRetryAfter time.Duration
}

// DefaultRetryPolicy 返回与官方 SDK 对齐的默认值:
// 最多重试 2 次;500ms 起步、2 倍退避、封顶 5s;jitter 25%;
// 重试 408/429/5xx、连接错误与超时;尊重 Retry-After,上限 60s。
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:            2,
		BackoffInitial:        500 * time.Millisecond,
		BackoffMax:            5 * time.Second,
		BackoffJitter:         0.25,
		RetryConnectionErrors: true,
		RetryTimeouts:         true,
		RespectRetryAfter:     true,
		MaxRetryAfter:         60 * time.Second,
	}
}

// retryStatus 报告状态码是否可重试。
func (p RetryPolicy) retryStatus(code int) bool {
	if p.HTTPStatuses != nil {
		return p.HTTPStatuses[code]
	}
	return code == http.StatusRequestTimeout || code == http.StatusTooManyRequests || code >= 500
}

// backoff 计算第 retry 次重试(从 0 计)前的等待时长,含 jitter。
func (p RetryPolicy) backoff(retry int) time.Duration {
	d := p.BackoffInitial << uint(retry)
	if d <= 0 || d > p.BackoffMax { // 位移溢出或超过上限时封顶
		d = p.BackoffMax
	}
	if p.BackoffJitter > 0 {
		d -= time.Duration(float64(d) * p.BackoffJitter * rand.Float64())
	}
	return d
}

// retryAfter 从响应头解析服务器建议的等待时长;未启用或无有效头时 ok 为 false。
func (p RetryPolicy) retryAfter(header http.Header) (time.Duration, bool) {
	if !p.RespectRetryAfter || header == nil {
		return 0, false
	}
	if v := strings.TrimSpace(header.Get("retry-after-ms")); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil && ms > 0 {
			return p.clampRetryAfter(time.Duration(ms) * time.Millisecond), true
		}
	}
	if v := strings.TrimSpace(header.Get("Retry-After")); v != "" {
		if secs, err := strconv.ParseFloat(v, 64); err == nil && secs > 0 {
			return p.clampRetryAfter(time.Duration(secs * float64(time.Second))), true
		}
		// HTTP 日期形式
		if at, err := http.ParseTime(v); err == nil {
			if d := time.Until(at); d > 0 {
				return p.clampRetryAfter(d), true
			}
		}
	}
	return 0, false
}

// clampRetryAfter 把服务器指示的等待时长限制在 MaxRetryAfter 内。
func (p RetryPolicy) clampRetryAfter(d time.Duration) time.Duration {
	if d > p.MaxRetryAfter {
		return p.MaxRetryAfter
	}
	return d
}

// sleepCtx 等待 d,或在 ctx 结束时提前返回其错误。
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
