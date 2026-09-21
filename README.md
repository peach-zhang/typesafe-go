# typesafe-go — TypeSafe System One 的 Go SDK

参考官方 JavaScript SDK(`TypeSafeClient` + `choice()/score()/noul()` 辅助函数)设计的
TypeSafe System One API(旗舰模型 Jev)Go 客户端。模型提供类型化的判断与概率,
代码掌控工作流。

## 安装

```sh
go get github.com/peach-zhang/typesafe-go
```

```go
import typesafe "github.com/peach-zhang/typesafe-go"
```

## 项目结构

```
typesafe-go/                # 包在仓库根,import 即模块路径
├── client.go               # Client、SystemOne、roundTrip、函数式选项
├── questions.go            # Noul / Choice / Score 问题构造
├── response.go             # SystemOneResult、Answer、Usage
├── models.go               # Models 端点与 ModelCard
├── retry.go                # RetryPolicy、指数退避、Retry-After
├── errors.go               # APIError / 连接 / 超时错误与判定函数
├── logger.go               # Logger 接口与 LogLevel
├── typesafe_test.go        # 单元测试(httptest 模拟服务端)
└── examples/triage/        # 工单分类示例
```

## 快速开始

```sh
# Windows PowerShell
$env:TYPESAFE_API_KEY = "你的key"
go run ./examples/triage
```

```go
client, err := typesafe.NewClient() // 读取环境变量 TYPESAFE_API_KEY

result, err := client.SystemOne(ctx, typesafe.SystemOneRequest{
    State: map[string]any{"ticket": map[string]any{"message": "Help! Payouts failing for 3 days."}},
    Questions: typesafe.Questions{
        "category": typesafe.Choice("Which team should handle `ticket.message`?", typesafe.ChoiceCriteria{
            "billing":   "Payments, invoicing, refunds",
            "technical": "Bugs, outages, integrations",
            "sales":     nil, // 无描述的选项
        }),
        "is_urgent":   typesafe.Noul("Does `ticket.message` convey urgency?", nil),
        "frustration": typesafe.Score("How frustrated is the customer?",
            typesafe.ScoreCriteria{"Calm", "Frustrated", "Very angry"}),
    },
})

category := result.Answers["category"]
// category.Choice        → "billing"
// category.Confidence    → 0.81
// category.Probability("technical") → 0.12, true
result.Answers["is_urgent"].Noul   // → 0.95
result.Answers["frustration"].Score // → 1.05(可落在两级之间)
```

## 与 JavaScript SDK 的 API 对照

| JavaScript SDK | 本 SDK (Go) |
|---|---|
| `new TypeSafeClient(config)` | `typesafe.NewClient(opts...)`(函数式选项) |
| `client.systemOne({state, questions, model?})` | `client.SystemOne(ctx, req)` |
| `client.models.list()` | `client.Models(ctx)` |
| `choice(instructions, criteria)` | `typesafe.Choice(instructions, criteria)` |
| `score(instructions, criteria)` | `typesafe.Score(instructions, criteria)` |
| `noul(instructions, criteria?)` | `typesafe.Noul(instructions, criteria)` |
| `answers.x.noul / .choice / .confidence` | `result.Answers["x"].Noul` 等 |
| `RetryPolicy`(maxRetries、backoff、jitter…) | `typesafe.RetryPolicy`(字段名对齐) |
| `AuthenticationError` / `RateLimitError` 等 | `typesafe.IsAuthenticationError(err)` 等判定函数 |
| key 解析:options → `TYPESAFE_API_KEY` | 相同顺序,空白值忽略 |
| 默认模型 `jev-latest`、URL 去尾斜杠 | 相同 |

## 模型列表

```go
cards, err := client.Models(ctx)
// cards[0].Name         → "jev-latest"
// cards[0].ReleaseDate  → "2026-09-10T18:38:01.391457+00:00"(RFC 3339)
// cards[0].Description  → 模型说明
```

`GET /v1/models` 目前只返回别名(如 `jev-latest`、`jev-preview`);
版本化的 ID(如 `jev-1.13.0`)即使未列出也可直接在 `model` 字段使用。

## 日志

```go
import "log"

client, _ := typesafe.NewClient(
    typesafe.WithLogger(log.New(os.Stderr, "", log.LstdFlags)),
    typesafe.WithLogLevel(typesafe.LogLevelDebug),
)
```

任何带 `Printf(format string, args ...any)` 方法的类型都满足 `Logger` 接口
(标准库 `*log.Logger` 直接可用)。级别从低到高:

| 级别 | 输出内容 |
|---|---|
| `LogLevelNone`(默认) | 完全静默 |
| `LogLevelError` | 最终失败(重试耗尽或不可重试错误) |
| `LogLevelInfo` | 调用摘要(`SystemOne 请求:模型 …,N 个问题`)与重试事件 |
| `LogLevelDebug` | 每次往返的方法、路径、字节数、耗时与问题 id 列表 |

日志**不包含 API key**;请求与响应只记录字节数,不记录内容,
避免 `state` 中的敏感信息进入日志。

## 客户端配置

```go
client, err := typesafe.NewClient(
    typesafe.WithAPIKey("..."),            // 或环境变量 TYPESAFE_API_KEY
    typesafe.WithBaseURL("https://api.typesafe.ai"),
    typesafe.WithDefaultModel("jev-latest"),
    typesafe.WithTimeout(30*time.Second),  // 单次尝试超时
    typesafe.WithRetryPolicy(policy),      // 见下
    typesafe.WithHeader("X-Custom", "v"),  // 附加默认头
    typesafe.WithHTTPClient(hc),           // 注入自定义 Transport
)
```

## 重试策略

默认与官方 SDK 对齐:最多重试 **2** 次;**500ms** 起步、2 倍指数退避、封顶 **5s**;
jitter **25%**;重试 408/429/5xx、连接错误与超时;尊重 `Retry-After` /
`retry-after-ms`(上限 60s)。4xx 客户端错误(401/403/422 等)不重试。

```go
policy := typesafe.DefaultRetryPolicy()
policy.MaxRetries = 4
client, _ := typesafe.NewClient(typesafe.WithRetryPolicy(policy))
```

## 错误处理

```go
result, err := client.SystemOne(ctx, req)
switch {
case typesafe.IsAuthenticationError(err): // 401,检查 API key
case typesafe.IsRateLimitError(err):      // 429,重试耗尽
case typesafe.IsUnprocessableEntityError(err): // 422,请求体校验失败
case typesafe.IsTimeoutError(err):        // 尝试超时且重试耗尽
case typesafe.IsConnectionError(err):     // 网络层失败
case err != nil:                          // 其他
}
```

错误链可透传 `context` 取消;`APIError` 携带 `StatusCode`、`Status`、
`Message`(响应体提取)与截断的 `Body` 便于诊断。

## 设计要点(来自官方文档)

- **一次调用尽管多问**:同一 `state` 上的问题并行评估,零额外延迟
- **`state` 只放相关上下文**,问题里用反引号路径引用嵌套值(如 `` `ticket.message` ``)
- **原子化分解问题**;choice 最多 255 选项,score 至少 2 级、最多 10 级(本地校验)
- **置信度用于路由**:`Confidence` 衡量概率分布集中度,低置信度送人工
- 测试:`go test ./...`;静态检查:`go vet ./... && gofmt -l .`

## 许可证

[MIT](LICENSE)
