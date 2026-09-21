package typesafe

// LogLevel 控制客户端输出哪些日志事件。
type LogLevel int

const (
	// LogLevelNone 不输出任何日志(默认)。
	LogLevelNone LogLevel = iota
	// LogLevelError 输出最终失败(重试耗尽或不可重试错误)。
	LogLevelError
	// LogLevelInfo 在 Error 基础上输出每次调用的摘要与重试事件。
	LogLevelInfo
	// LogLevelDebug 在 Info 基础上输出每次 HTTP 往返的明细(方法、字节数、耗时)。
	LogLevelDebug
)

// String 返回级别名称,便于日志排查。
func (l LogLevel) String() string {
	switch l {
	case LogLevelError:
		return "error"
	case LogLevelInfo:
		return "info"
	case LogLevelDebug:
		return "debug"
	default:
		return "none"
	}
}

// Logger 是最小日志接口,标准库 *log.Logger 满足它。
//
// 日志内容不包含 API key;请求与响应只记录字节数,不记录内容,
// 避免 state 中的敏感信息进入日志。
type Logger interface {
	Printf(format string, args ...any)
}

// WithLogger 注入日志记录器;不设置则完全静默。
func WithLogger(logger Logger) ClientOption {
	return func(c *Client) { c.logger = logger }
}

// WithLogLevel 设置日志级别,默认 LogLevelNone。
// 级别与记录器都应在 NewClient 时确定,之后不再修改。
func WithLogLevel(level LogLevel) ClientOption {
	return func(c *Client) { c.logLevel = level }
}

// logf 在达到级别时输出一条带 typesafe 前缀的日志。
func (c *Client) logf(level LogLevel, format string, args ...any) {
	if c.logger == nil || c.logLevel < level {
		return
	}
	c.logger.Printf("typesafe: "+format, args...)
}
