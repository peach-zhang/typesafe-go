package typesafe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient 返回指向 ts 的客户端,opts 附加在基础配置之后。
func newTestClient(t *testing.T, ts *httptest.Server, opts ...ClientOption) *Client {
	t.Helper()
	args := append([]ClientOption{WithAPIKey("test-key"), WithBaseURL(ts.URL)}, opts...)
	c, err := NewClient(args...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// fastRetry 返回不拖慢测试的重试策略。
func fastRetry() RetryPolicy {
	p := DefaultRetryPolicy()
	p.BackoffInitial = time.Millisecond
	p.BackoffMax = 2 * time.Millisecond
	p.BackoffJitter = 0
	p.RespectRetryAfter = false
	return p
}

const okBody = `{
	"model": "jev-1.13.0",
	"answers": {
		"is_urgent": {"type": "noul", "noul": 0.95},
		"category": {
			"type": "choice",
			"choice": "billing",
			"probabilities": {"billing": 0.88, "technical": 0.12},
			"confidence": 0.81
		},
		"frustration": {
			"type": "score",
			"score": 1.05,
			"legend": {"0": "Calm", "1": "Frustrated", "2": "Very angry"},
			"probabilities": {"0": 0.0, "1": 0.95, "2": 0.05},
			"confidence": 0.92
		}
	},
	"usage": {"input_tokens": 296, "output_tokens": 20}
}`

func sampleQuestions() Questions {
	return Questions{
		"is_urgent":   Noul("Does this convey urgency?", nil),
		"category":    Choice("Which team should handle this?", ChoiceCriteria{"billing": "Payments", "technical": "Bugs", "sales": nil}),
		"frustration": Score("How frustrated is the customer?", ScoreCriteria{"Calm", "Frustrated", "Very angry"}),
	}
}

func TestSystemOneRequestShape(t *testing.T) {
	var got map[string]json.RawMessage
	var auth, contentType, method, path string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		auth = r.Header.Get("Authorization")
		contentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("解析请求体: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(okBody))
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	if _, err := c.SystemOne(context.Background(), SystemOneRequest{
		State:     map[string]any{"document": "I was charged twice."},
		Questions: sampleQuestions(),
	}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}

	if method != http.MethodPost || path != "/v1/systemone" {
		t.Errorf("请求 = %s %s, 期望 POST /v1/systemone", method, path)
	}
	if auth != "Bearer test-key" {
		t.Errorf("Authorization = %q, 期望 Bearer test-key", auth)
	}
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q", contentType)
	}

	// 默认模型应填充 jev-latest
	var model string
	if err := json.Unmarshal(got["model"], &model); err != nil || model != "jev-latest" {
		t.Errorf("model = %q (%v), 期望 jev-latest", model, err)
	}

	// state 原样传递
	if !strings.Contains(string(got["state"]), "charged twice") {
		t.Errorf("state 缺少文档内容: %s", got["state"])
	}

	// 三种问题的线形状
	var questions map[string]struct {
		Type         string          `json:"type"`
		Instructions json.RawMessage `json:"instructions"`
		Criteria     json.RawMessage `json:"criteria"`
	}
	if err := json.Unmarshal(got["questions"], &questions); err != nil {
		t.Fatalf("解析 questions: %v", err)
	}
	if q := questions["is_urgent"]; q.Type != "noul" || string(q.Criteria) != "" {
		t.Errorf("noul 形状异常: %+v", q)
	}
	if q := questions["category"]; q.Type != "choice" || !strings.Contains(string(q.Criteria), `"sales":null`) {
		t.Errorf("choice 形状异常: %+v", q)
	}
	if q := questions["frustration"]; q.Type != "score" || !strings.Contains(string(q.Criteria), "Very angry") {
		t.Errorf("score 形状异常: %+v", q)
	}
}

func TestAnswersParsing(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(okBody))
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	result, err := c.SystemOne(context.Background(), SystemOneRequest{
		State:     "test",
		Questions: sampleQuestions(),
	})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}

	if result.Model != "jev-1.13.0" {
		t.Errorf("Model = %q", result.Model)
	}
	if result.Usage.InputTokens != 296 || result.Usage.OutputTokens != 20 {
		t.Errorf("Usage = %+v", result.Usage)
	}

	urgent := result.Answers["is_urgent"]
	if urgent.Type != "noul" || urgent.Noul != 0.95 {
		t.Errorf("noul 答案 = %+v", urgent)
	}

	category := result.Answers["category"]
	if category.Choice != "billing" || category.Confidence != 0.81 {
		t.Errorf("choice 答案 = %+v", category)
	}
	if p, ok := category.Probability("technical"); !ok || p != 0.12 {
		t.Errorf("Probability(technical) = %v, %v", p, ok)
	}

	frustration := result.Answers["frustration"]
	if frustration.Score != 1.05 || frustration.Legend["2"] != "Very angry" {
		t.Errorf("score 答案 = %+v", frustration)
	}
}

func TestEmptyQuestions(t *testing.T) {
	c, err := NewClient(WithAPIKey("k"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = c.SystemOne(context.Background(), SystemOneRequest{State: "x"})
	if err == nil || !strings.Contains(err.Error(), "questions 不能为空") {
		t.Errorf("期望空 questions 报错,得到 %v", err)
	}
}

func TestInvalidQuestions(t *testing.T) {
	c, err := NewClient(WithAPIKey("k"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	cases := []struct {
		name string
		q    Questions
		want string
	}{
		{"score 等级不足", Questions{"mood": Score("?", ScoreCriteria{"low"})}, "至少需要 2 个等级"},
		{"score 等级过多", Questions{"mood": Score("?", ScoreCriteria{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"})}, "超过上限 10"},
		{"choice 缺选项", Questions{"topic": Choice("?", nil)}, "缺少 criteria"},
		{"noul 缺指令", Questions{"x": Noul(nil, nil)}, "缺少 instructions"},
	}
	for _, tc := range cases {
		_, err := c.SystemOne(context.Background(), SystemOneRequest{State: "x", Questions: tc.q})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: 期望含 %q 的错误,得到 %v", tc.name, tc.want, err)
		}
	}
}

func TestRetryOn429ThenSuccess(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, `{"message":"rate limited"}`, http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(okBody))
	}))
	defer ts.Close()

	c := newTestClient(t, ts, WithRetryPolicy(fastRetry()))
	result, err := c.SystemOne(context.Background(), SystemOneRequest{
		State:     "test",
		Questions: sampleQuestions(),
	})
	if err != nil {
		t.Fatalf("重试后应成功,得到 %v", err)
	}
	if result.Answers["is_urgent"].Noul != 0.95 {
		t.Errorf("答案异常: %+v", result.Answers["is_urgent"])
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("调用次数 = %d, 期望 3(1 次失败 + 2 次重试)", got)
	}
}

func TestRetriesExhaustedReturnsLastError(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, `{"message":"slow down"}`, http.StatusTooManyRequests)
	}))
	defer ts.Close()

	c := newTestClient(t, ts, WithRetryPolicy(fastRetry()))
	_, err := c.SystemOne(context.Background(), SystemOneRequest{
		State:     "test",
		Questions: sampleQuestions(),
	})
	if !IsRateLimitError(err) {
		t.Fatalf("期望 RateLimitError,得到 %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("调用次数 = %d, 期望 3", got)
	}
}

func TestAuthenticationError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"invalid api key"}`, http.StatusUnauthorized)
	}))
	defer ts.Close()

	c := newTestClient(t, ts, WithRetryPolicy(fastRetry()))
	_, err := c.SystemOne(context.Background(), SystemOneRequest{
		State:     "test",
		Questions: Questions{"x": Noul("?", nil)},
	})
	if !IsAuthenticationError(err) {
		t.Fatalf("期望 AuthenticationError,得到 %v", err)
	}
}

func TestNoRetryOn422(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, `{"detail":"validation failed"}`, http.StatusUnprocessableEntity)
	}))
	defer ts.Close()

	c := newTestClient(t, ts, WithRetryPolicy(fastRetry()))
	_, err := c.SystemOne(context.Background(), SystemOneRequest{
		State:     "test",
		Questions: Questions{"x": Noul("?", nil)},
	})
	if !IsUnprocessableEntityError(err) {
		t.Fatalf("期望 UnprocessableEntityError,得到 %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("422 不应重试,调用次数 = %d", got)
	}
}

func TestContextCancelDuringRetry(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer ts.Close()

	// 用长退避确保取消发生在等待期间
	p := fastRetry()
	p.BackoffInitial = 10 * time.Second
	p.BackoffMax = 20 * time.Second

	c := newTestClient(t, ts, WithRetryPolicy(p))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.SystemOne(ctx, SystemOneRequest{
		State:     "test",
		Questions: Questions{"x": Noul("?", nil)},
	})
	if err == nil {
		t.Fatal("期望因取消而失败")
	}
	if !strings.Contains(err.Error(), "重试等待被取消") && ctx.Err() == nil {
		t.Errorf("意外错误: %v", err)
	}
}

func TestNewClientEnvFallback(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "  env-key  ")
	c, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.apiKey != "env-key" {
		t.Errorf("apiKey = %q, 期望去除空白后的 env-key", c.apiKey)
	}
}

func TestNewClientMissingKey(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	if _, err := NewClient(); err == nil {
		t.Fatal("缺少 API key 时应返回错误")
	}
}

func TestDefaultRetryPolicy(t *testing.T) {
	p := DefaultRetryPolicy()
	if p.MaxRetries != 2 || p.BackoffInitial != 500*time.Millisecond || p.BackoffMax != 5*time.Second {
		t.Errorf("默认策略与官方 SDK 不一致: %+v", p)
	}
	if !p.retryStatus(408) || !p.retryStatus(429) || !p.retryStatus(500) || !p.retryStatus(503) {
		t.Errorf("408/429/5xx 应可重试")
	}
	if p.retryStatus(400) || p.retryStatus(401) || p.retryStatus(422) {
		t.Errorf("4xx 客户端错误不应重试")
	}
}

func TestBackoffDoublingAndCap(t *testing.T) {
	p := DefaultRetryPolicy()
	p.BackoffJitter = 0
	got := []time.Duration{p.backoff(0), p.backoff(1), p.backoff(2), p.backoff(3), p.backoff(4)}
	want := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("backoff(%d) = %v, 期望 %v", i, got[i], want[i])
		}
	}
}

func TestModelsList(t *testing.T) {
	var method, path, auth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		w.Write([]byte(`{"models":[
			{"name":"jev-latest","description":"Stable release","release_date":"2026-08-01"},
			{"name":"jev-preview","description":"Most recent build","release_date":"2026-09-15"}
		]}`))
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	cards, err := c.Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if method != http.MethodGet || path != "/v1/models" {
		t.Errorf("请求 = %s %s, 期望 GET /v1/models", method, path)
	}
	if auth != "Bearer test-key" {
		t.Errorf("Authorization = %q", auth)
	}
	if len(cards) != 2 {
		t.Fatalf("模型数 = %d, 期望 2", len(cards))
	}
	first := cards[0]
	if first.Name != "jev-latest" || first.Description != "Stable release" || first.ReleaseDate != "2026-08-01" {
		t.Errorf("第一张模型卡片 = %+v", first)
	}
}

func TestModelsRetriesOn429(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{"models":[{"name":"jev-latest","description":"Stable","release_date":"2026-08-01"}]}`))
	}))
	defer ts.Close()

	c := newTestClient(t, ts, WithRetryPolicy(fastRetry()))
	cards, err := c.Models(context.Background())
	if err != nil {
		t.Fatalf("重试后应成功: %v", err)
	}
	if len(cards) != 1 || cards[0].Name != "jev-latest" {
		t.Errorf("模型卡片 = %+v", cards)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("调用次数 = %d, 期望 2", got)
	}
}

// recordingLogger 收集所有日志行,便于断言。
type recordingLogger struct {
	mu    sync.Mutex
	lines []string
}

func (l *recordingLogger) Printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *recordingLogger) all() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

func TestLoggerDebug(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(okBody))
	}))
	defer ts.Close()

	logger := &recordingLogger{}
	c := newTestClient(t, ts, WithLogger(logger), WithLogLevel(LogLevelDebug))
	if _, err := c.SystemOne(context.Background(), SystemOneRequest{
		State:     "test",
		Questions: sampleQuestions(),
	}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}

	out := logger.all()
	for _, want := range []string{"SystemOne 请求", "POST /v1/systemone", "成功", "问题列表"} {
		if !strings.Contains(out, want) {
			t.Errorf("debug 日志缺少 %q:\n%s", want, out)
		}
	}
	// 日志不得包含 API key
	if strings.Contains(out, "test-key") {
		t.Errorf("日志泄露了 API key:\n%s", out)
	}
}

func TestLoggerSilentByDefault(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(okBody))
	}))
	defer ts.Close()

	logger := &recordingLogger{}
	c := newTestClient(t, ts, WithLogger(logger)) // 未设级别,默认 LogLevelNone
	if _, err := c.SystemOne(context.Background(), SystemOneRequest{
		State:     "test",
		Questions: sampleQuestions(),
	}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	if out := logger.all(); out != "" {
		t.Errorf("默认应静默,但输出了:\n%s", out)
	}
}

func TestLoggerRetryEvent(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(okBody))
	}))
	defer ts.Close()

	logger := &recordingLogger{}
	c := newTestClient(t, ts, WithLogger(logger), WithLogLevel(LogLevelInfo), WithRetryPolicy(fastRetry()))
	if _, err := c.SystemOne(context.Background(), SystemOneRequest{
		State:     "test",
		Questions: sampleQuestions(),
	}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}

	out := logger.all()
	if !strings.Contains(out, "第 1 次尝试失败") || !strings.Contains(out, "重试") {
		t.Errorf("info 日志缺少重试事件:\n%s", out)
	}
	if strings.Contains(out, "POST /v1/systemone(") { // debug 级明细不应出现在 info
		t.Errorf("info 级别不应包含 debug 明细:\n%s", out)
	}
}

func TestRetryAfterHeaderParsing(t *testing.T) {
	p := DefaultRetryPolicy()

	h := http.Header{}
	h.Set("Retry-After", "2")
	if d, ok := p.retryAfter(h); !ok || d != 2*time.Second {
		t.Errorf("Retry-After=2 → %v, %v", d, ok)
	}

	h = http.Header{}
	h.Set("retry-after-ms", "250")
	if d, ok := p.retryAfter(h); !ok || d != 250*time.Millisecond {
		t.Errorf("retry-after-ms=250 → %v, %v", d, ok)
	}

	// 超过上限时封顶
	h = http.Header{}
	h.Set("Retry-After", "999")
	if d, ok := p.retryAfter(h); !ok || d != p.MaxRetryAfter {
		t.Errorf("超限 Retry-After → %v, %v", d, ok)
	}

	// 未启用时不解析
	p.RespectRetryAfter = false
	if _, ok := p.retryAfter(h); ok {
		t.Error("禁用 RespectRetryAfter 后不应解析")
	}
}
