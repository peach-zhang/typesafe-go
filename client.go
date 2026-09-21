// Package typesafe 是 TypeSafe System One API(旗舰模型 Jev)的 Go 客户端。
//
// 设计对齐官方 JavaScript SDK(TypeSafeClient + choice()/score()/noul()
// 辅助函数),并用 Go 惯用法表达:函数式选项替代 config 对象,
// context.Context 贯穿调用与重试,密封接口约束问题构造。
//
// 基本用法:
//
//	client, err := typesafe.NewClient() // 从环境变量 TYPESAFE_API_KEY 读取
//	if err != nil { ... }
//
//	result, err := client.SystemOne(ctx, typesafe.SystemOneRequest{
//	    State: map[string]any{"document": "I was charged twice."},
//	    Questions: typesafe.Questions{
//	        "is_urgent": typesafe.Noul("Does this convey urgency?", nil),
//	        "category": typesafe.Choice("What is this ticket about?", typesafe.ChoiceCriteria{
//	            "billing":   "Payments, invoicing, refunds",
//	            "technical": "Bugs, outages, integrations",
//	        }),
//	    },
//	})
//	if err != nil { ... }
//	p := result.Answers["is_urgent"].Noul
package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// 默认配置,与官方 SDK 对齐。
const (
	// DefaultBaseURL 是 API 根地址。
	DefaultBaseURL = "https://api.typesafe.ai"
	// DefaultModel 是请求未指定模型时使用的默认模型。
	DefaultModel = "jev-latest"
	// DefaultTimeout 是单次请求尝试的超时。
	DefaultTimeout = 60 * time.Second

	apiKeyEnv    = "TYPESAFE_API_KEY"
	systemOneURL = "/v1/systemone"
)

// Client 是 System One API 客户端,可安全地在多个 goroutine 间共享。
// 日志级别与记录器应在 NewClient 时确定,之后不再修改。
type Client struct {
	apiKey         string
	baseURL        string
	defaultModel   string
	defaultHeaders map[string]string
	timeout        time.Duration
	retry          RetryPolicy
	httpClient     *http.Client
	logger         Logger
	logLevel       LogLevel
}

// ClientOption 配置 Client,优先级高于环境变量与默认值。
type ClientOption func(*Client)

// WithAPIKey 设置 API key;未设置时回退到环境变量 TYPESAFE_API_KEY。
func WithAPIKey(key string) ClientOption {
	return func(c *Client) { c.apiKey = key }
}

// WithBaseURL 覆盖 API 根地址(用于代理或私有部署),尾部的斜杠会被去掉。
func WithBaseURL(rawURL string) ClientOption {
	return func(c *Client) { c.baseURL = strings.TrimRight(rawURL, "/") }
}

// WithDefaultModel 设置请求省略 Model 字段时使用的模型。
func WithDefaultModel(model string) ClientOption {
	return func(c *Client) { c.defaultModel = model }
}

// WithHeader 追加一个随每个请求发送的 HTTP 头。
func WithHeader(key, value string) ClientOption {
	return func(c *Client) {
		if c.defaultHeaders == nil {
			c.defaultHeaders = map[string]string{}
		}
		c.defaultHeaders[key] = value
	}
}

// WithTimeout 设置单次请求尝试的超时。
func WithTimeout(d time.Duration) ClientOption {
	return func(c *Client) { c.timeout = d }
}

// WithRetryPolicy 覆盖重试策略。
func WithRetryPolicy(p RetryPolicy) ClientOption {
	return func(c *Client) { c.retry = p }
}

// WithHTTPClient 替换底层 HTTP 客户端(便于注入自定义 Transport)。
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *Client) { c.httpClient = hc }
}

// NewClient 创建客户端。API key 的解析顺序与官方 SDK 一致:
// WithAPIKey 显式传入 → 环境变量 TYPESAFE_API_KEY(空白值忽略)。
// 两者都缺失时返回错误。
func NewClient(opts ...ClientOption) (*Client, error) {
	c := &Client{
		baseURL:        DefaultBaseURL,
		defaultModel:   DefaultModel,
		timeout:        DefaultTimeout,
		retry:          DefaultRetryPolicy(),
		httpClient:     &http.Client{},
		defaultHeaders: map[string]string{},
	}
	for _, opt := range opts {
		opt(c)
	}
	if strings.TrimSpace(c.apiKey) == "" {
		c.apiKey = strings.TrimSpace(os.Getenv(apiKeyEnv))
	}
	if c.apiKey == "" {
		return nil, fmt.Errorf("typesafe: 缺少 API key:请通过 WithAPIKey 传入或设置环境变量 %s", apiKeyEnv)
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{}
	}
	return c, nil
}

// SystemOneRequest 描述一次评估请求。
type SystemOneRequest struct {
	// State 是待评估的上下文:文本、嵌套 JSON 对象或数组。
	// 只放与当前问题相关的信息,问题里用反引号路径引用嵌套值,
	// 例如 "Is `ticket.messages[0].text` about billing?"。
	State any
	// Model 可选;为空时使用客户端的默认模型(jev-latest)。
	Model string
	// Questions 是问题 id 到问题的映射,不可为空。
	Questions Questions
}

// wireRequest 是请求体的 JSON 形状。
type wireRequest struct {
	State     any       `json:"state"`
	Model     string    `json:"model"`
	Questions Questions `json:"questions"`
}

// SystemOne 把状态与一组类型化问题发给 System One 模型并返回答案集合。
//
// 同一 state 上的问题会被并行评估,一次调用尽管多问。
// 问题为空、score 等级不足 2、choice 缺少选项等约束冲突会在本地直接报错;
// 限流(429)、超时与瞬时服务端错误按 RetryPolicy 自动重试。
func (c *Client) SystemOne(ctx context.Context, req SystemOneRequest) (*SystemOneResult, error) {
	if len(req.Questions) == 0 {
		return nil, errors.New("typesafe: questions 不能为空")
	}
	for id, q := range req.Questions {
		if err := q.validate(); err != nil {
			return nil, fmt.Errorf("typesafe: 问题 %q 无效: %w", id, err)
		}
	}

	model := req.Model
	if model == "" {
		model = c.defaultModel
	}

	body, err := json.Marshal(wireRequest{State: req.State, Model: model, Questions: req.Questions})
	if err != nil {
		return nil, fmt.Errorf("typesafe: 序列化请求失败: %w", err)
	}

	c.logf(LogLevelInfo, "SystemOne 请求:模型 %s,%d 个问题", model, len(req.Questions))
	if c.logLevel >= LogLevelDebug {
		ids := make([]string, 0, len(req.Questions))
		for id := range req.Questions {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		c.logf(LogLevelDebug, "问题列表: %v", ids)
	}

	var result SystemOneResult
	if err := c.roundTrip(ctx, http.MethodPost, systemOneURL, body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// roundTrip 执行带重试的 HTTP 往返,成功时把 JSON 响应解析进 out。
func (c *Client) roundTrip(ctx context.Context, method, path string, body []byte, out any) error {
	url := c.baseURL + path
	c.logf(LogLevelDebug, "%s %s(%d 字节请求体)", method, path, len(body))

	for attempt := 0; ; attempt++ {
		start := time.Now()
		respBody, retryAfter, retryable, err := c.attempt(ctx, method, url, body)
		if err == nil {
			c.logf(LogLevelDebug, "%s %s 成功(%d 字节响应,耗时 %s)",
				method, path, len(respBody), time.Since(start).Round(time.Millisecond))
			if err := json.Unmarshal(respBody, out); err != nil {
				return fmt.Errorf("typesafe: 解析响应失败: %w", err)
			}
			return nil
		}
		if !retryable || attempt >= c.retry.MaxRetries {
			c.logf(LogLevelError, "%s %s 失败: %v", method, path, err)
			return err
		}

		// 取本地退避与服务器指示(Retry-After)中的较大者
		delay := c.retry.backoff(attempt)
		if ra, ok := retryAfter(); ok && ra > delay {
			delay = ra
		}
		c.logf(LogLevelInfo, "%s %s 第 %d 次尝试失败(%v),%s 后重试",
			method, path, attempt+1, err, delay)
		if waitErr := sleepCtx(ctx, delay); waitErr != nil {
			return fmt.Errorf("typesafe: 重试等待被取消: %w", waitErr)
		}
	}
}

// attempt 执行单次请求尝试,成功时返回原始响应体。retryAfter 仅在返回
// 可重试错误时非 nil,供调用方在退避前解析服务器建议的等待时长。
func (c *Client) attempt(ctx context.Context, method, url string, body []byte) ([]byte, func() (time.Duration, bool), bool, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	httpReq, err := http.NewRequestWithContext(attemptCtx, method, url, reader)
	if err != nil {
		return nil, nil, false, fmt.Errorf("typesafe: 构造请求失败: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	for k, v := range c.defaultHeaders {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		// 调用方主动取消:立即失败,不重试
		if ctx.Err() != nil {
			return nil, nil, false, ctx.Err()
		}
		// 本次尝试超时:按策略决定是否重试
		if attemptCtx.Err() != nil {
			return nil, nil, c.retry.RetryTimeouts, &APITimeoutError{Err: attemptCtx.Err()}
		}
		return nil, nil, c.retry.RetryConnectionErrors, &APIConnectionError{Err: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		connErr := &APIConnectionError{Err: fmt.Errorf("读取响应体中断: %w", err)}
		return nil, nil, c.retry.RetryConnectionErrors, connErr
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Message:    extractMessage(respBody),
			Body:       truncate(string(respBody), 2048),
		}
		if !c.retry.retryStatus(resp.StatusCode) {
			return nil, nil, false, apiErr
		}
		header := resp.Header
		return nil, func() (time.Duration, bool) { return c.retry.retryAfter(header) }, true, apiErr
	}

	return respBody, nil, false, nil
}

// extractMessage 尝试从错误响应体提取常见的错误说明字段。
func extractMessage(body []byte) string {
	var payload struct {
		Message string `json:"message"`
		Error   string `json:"error"`
		Detail  string `json:"detail"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	for _, m := range []string{payload.Message, payload.Error, payload.Detail} {
		if m != "" {
			return m
		}
	}
	return ""
}

// truncate 把超出 n 字节的部分替换为省略标记。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…(已截断)"
}
