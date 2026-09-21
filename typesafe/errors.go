package typesafe

import (
	"errors"
	"fmt"
)

// APIError 表示服务端返回了非 2xx 响应。
type APIError struct {
	// StatusCode 是 HTTP 状态码。
	StatusCode int
	// Status 是状态行文本,例如 "429 Too Many Requests"。
	Status string
	// Message 是从响应体提取的错误说明(若有)。
	Message string
	// Body 是原始响应体(截断),便于诊断。
	Body string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("typesafe: API 错误 %d %s: %s", e.StatusCode, e.Status, e.Message)
	}
	return fmt.Sprintf("typesafe: API 错误 %d %s", e.StatusCode, e.Status)
}

// APIConnectionError 表示网络层失败(连接被拒、DNS 失败、中断的响应体等)。
type APIConnectionError struct {
	// Err 是底层网络错误。
	Err error
}

func (e *APIConnectionError) Error() string {
	return fmt.Sprintf("typesafe: 连接错误: %v", e.Err)
}

func (e *APIConnectionError) Unwrap() error { return e.Err }

// APITimeoutError 表示单次请求尝试超时。
type APITimeoutError struct {
	// Err 是底层超时错误。
	Err error
}

func (e *APITimeoutError) Error() string {
	return fmt.Sprintf("typesafe: 请求超时: %v", e.Err)
}

func (e *APITimeoutError) Unwrap() error { return e.Err }

// IsAuthenticationError 判断错误是否为 401(API key 缺失或无效)。
func IsAuthenticationError(err error) bool { return hasStatus(err, 401) }

// IsPermissionDeniedError 判断错误是否为 403。
func IsPermissionDeniedError(err error) bool { return hasStatus(err, 403) }

// IsNotFoundError 判断错误是否为 404。
func IsNotFoundError(err error) bool { return hasStatus(err, 404) }

// IsRateLimitError 判断错误是否为 429(限流)。
func IsRateLimitError(err error) bool { return hasStatus(err, 429) }

// IsUnprocessableEntityError 判断错误是否为 422(请求体校验失败)。
func IsUnprocessableEntityError(err error) bool { return hasStatus(err, 422) }

// IsConnectionError 判断错误是否为网络连接类错误。
func IsConnectionError(err error) bool {
	var connErr *APIConnectionError
	return errors.As(err, &connErr)
}

// IsTimeoutError 判断错误是否为超时错误。
func IsTimeoutError(err error) bool {
	var timeoutErr *APITimeoutError
	return errors.As(err, &timeoutErr)
}

func hasStatus(err error, code int) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == code
}
