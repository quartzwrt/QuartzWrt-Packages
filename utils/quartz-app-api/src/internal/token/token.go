// Package token 实现 access_token 的 JWT（HS256）签发与校验（DESIGN §3.1）。
//
// 零第三方依赖：header/claims 的 base64url 编码 + HMAC-SHA256 全部 stdlib。
// 设计要点：
//   - exp 校验允许 ±skew 时钟偏斜（无 RTC 机型，DESIGN §3.2）；
//   - 过期与非法严格区分（过期→App 静默 refresh；非法→强制重配对）；
//   - 签名比较恒定时间。
package token

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Issuer access_token 的 iss 固定值。
const Issuer = "quartz-app-api"

// Skew 允许的时钟偏斜（DESIGN §3.2）。
const Skew = 5 * time.Minute

// 校验失败原因（区分处理）。
var (
	ErrExpired = errors.New("token expired")
	ErrInvalid = errors.New("token invalid")
)

// Claims access_token 载荷。
type Claims struct {
	Iss string `json:"iss"`
	Sub string `json:"sub"` // device_id
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
	Jti string `json:"jti"`
}

var header = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

// Sign 签发 access_token。
func Sign(secret []byte, c Claims) (string, error) {
	body, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	signing := header + "." + base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signing))
	return signing + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// Verify 校验 access_token：签名（恒定时间）→ iss → exp（±Skew）。
func Verify(secret []byte, tok string, now time.Time) (Claims, error) {
	var c Claims
	parts := strings.Split(tok, ".")
	if len(parts) != 3 || parts[0] != header {
		return c, ErrInvalid
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	want := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || subtle.ConstantTimeCompare(want, got) != 1 {
		return c, ErrInvalid
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return c, ErrInvalid
	}
	if err := json.Unmarshal(body, &c); err != nil {
		return c, ErrInvalid
	}
	if c.Iss != Issuer || c.Sub == "" {
		return c, ErrInvalid
	}
	// iat 在未来（时钟回拨）→ 视为非法（DESIGN §3.2：走 refresh 路径）。
	if c.Iat > now.Add(Skew).Unix() {
		return c, ErrInvalid
	}
	if now.After(time.Unix(c.Exp, 0).Add(Skew)) {
		return c, ErrExpired
	}
	return c, nil
}

// NewJTI 生成随机 jti（16 字节 hex）。
func NewJTI() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// NewRefresh 生成 refresh_token 明文（32 字节随机，base64url）。
func NewRefresh() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashRefresh refresh_token 的存储形态（只存 SHA-256 hex，DESIGN §3.1）。
func HashRefresh(refresh string) string {
	sum := sha256.Sum256([]byte(refresh))
	return hex.EncodeToString(sum[:])
}
