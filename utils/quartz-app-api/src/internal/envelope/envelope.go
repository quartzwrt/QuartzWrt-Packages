// Package envelope 定义 quartz-app-api 的响应信封与错误码（DESIGN §4.2/§4.3）。
//
// rpcd exec 插件机制：stdout 为合法 JSON 对象即视为成功（退出码与 stderr 均
// 不影响回复，DESIGN §1.1），因此**方法级错误必须 in-band**——统一信封：
//
//	{"ok":true,  "api":1, ...载荷...}
//	{"ok":false, "api":1, "error":{"code":"...","message":"...", ...}}
package envelope

// API 主 API 版本（info.api；破坏性变更才递增，DESIGN §4.2）。
const API = 1

// 错误码全表（DESIGN §4.3）。
const (
	CodeInvalidCredentials = "invalid_credentials"
	CodeRateLimited        = "rate_limited"
	CodeTokenExpired       = "token_expired"
	CodeTokenInvalid       = "token_invalid"
	CodeDeviceRevoked      = "device_revoked"
	CodeForbiddenAction    = "forbidden_action"
	CodeActionFailed       = "action_failed"
	CodeDataUnavailable    = "data_unavailable"
	CodeBadRequest         = "bad_request"
	CodeInternal           = "internal"
)

// Error 机器可读错误体（App 按 code 分派行为，message/detail 供展示与排障）。
type Error struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	RetryAfter int    `json:"retry_after,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// Error 实现 error 接口（便于内部传递）。
func (e *Error) Error() string { return e.Code + ": " + e.Message }

// E 构造错误（message 为短英文机器串，本地化由 App 做）。
func E(code, message string) *Error { return &Error{Code: code, Message: message} }

// ED 构造带 detail 的错误。
func ED(code, message, detail string) *Error {
	return &Error{Code: code, Message: message, Detail: detail}
}

// OK 组装成功信封（payload 键合并进顶层）。
func OK(payload map[string]any) map[string]any {
	out := map[string]any{"ok": true, "api": API}
	for k, v := range payload {
		out[k] = v
	}
	return out
}

// Err 组装失败信封。
func Err(e *Error) map[string]any {
	return map[string]any{"ok": false, "api": API, "error": e}
}
