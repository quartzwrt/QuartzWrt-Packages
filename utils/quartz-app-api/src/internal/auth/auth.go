// Package auth 认证核心：login / refresh / logout / devices / revoke /
// authenticate（DESIGN §3、§4.1）。
//
// 状态机要点：
//   - access_token 无状态 JWT（token 包），每次调用复核设备注册表（吊销即时生效）；
//   - refresh_token 旋转：成功后旧 refresh 变 PrevRefreshHash；**再次出现即判定
//     泄露复用 → 吊销该设备**（DESIGN §3.1）；
//   - 登录失败走全局限速（ratelimit 包；v1.0.1：/ubus 不转发客户端 IP，全局键）；
//   - 时钟：exp 判读带偏斜（token 包）；refresh 绝对上限自设备创建起算。
package auth

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"time"
	"unicode"

	"github.com/quartzwrt/quartz-app-api/internal/audit"
	"github.com/quartzwrt/quartz-app-api/internal/config"
	"github.com/quartzwrt/quartz-app-api/internal/envelope"
	"github.com/quartzwrt/quartz-app-api/internal/ratelimit"
	"github.com/quartzwrt/quartz-app-api/internal/store"
	"github.com/quartzwrt/quartz-app-api/internal/token"
)

// CredValidator 校验系统管理员凭据（生产实现：rpcd session.login 探测，
// 见 cmd/quartz-app-api；测试注入假实现）。
type CredValidator func(ctx context.Context, username, password string) error

// Service 认证服务（无内部状态，状态全在 store/limiter 文件）。
type Service struct {
	Store    *store.Store
	Limiter  *ratelimit.Limiter
	Cfg      config.Config
	Now      func() time.Time
	Validate CredValidator
	Router   func() map[string]any // 身份信息（hostinfo.Info；测试可注入）
	// TouchWindow 秒：Authenticate 刷新 last_seen 的节流窗口（省 flash 写）。
	TouchWindow int64
}

// New 构造服务（Now/TouchWindow 缺省）。
func New(st *store.Store, lim *ratelimit.Limiter, cfg config.Config, v CredValidator) *Service {
	return &Service{
		Store:       st,
		Limiter:     lim,
		Cfg:         cfg,
		Now:         time.Now,
		Validate:    v,
		TouchWindow: 3600,
	}
}

// 内部哨兵。
var (
	errUnknownDevice = errors.New("unknown device")
	errRevoked       = errors.New("device revoked")
	errBadRefresh    = errors.New("bad refresh token")
	errReuse         = errors.New("refresh token reuse")
	errRefreshExp    = errors.New("refresh token expired")
)

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) router() map[string]any {
	if s.Router != nil {
		return s.Router()
	}
	return map[string]any{}
}

// Login 管理员凭据换双令牌（DESIGN §4.1 login）。
// clientIP 由调用方尽力提供（仅作展示元数据，不参与任何安全决策）。
func (s *Service) Login(ctx context.Context, username, password, deviceName, clientIP string) (map[string]any, *envelope.Error) {
	now := s.now()
	if locked, retry := s.Limiter.Check(ratelimit.GlobalKey, now); locked {
		audit.Logf("login rate_limited retry_after=%d", retry)
		return nil, &envelope.Error{Code: envelope.CodeRateLimited, Message: "too many failed attempts", RetryAfter: retry}
	}
	if s.Validate == nil || s.Validate(ctx, username, password) != nil {
		_ = s.Limiter.Fail(ratelimit.GlobalKey, now)
		audit.Logf("login fail user=%q", username)
		return nil, envelope.E(envelope.CodeInvalidCredentials, "invalid credentials")
	}
	_ = s.Limiter.Reset(ratelimit.GlobalKey, now)

	secret, err := s.Store.Secret()
	if err != nil {
		return nil, envelope.ED(envelope.CodeInternal, "secret unavailable", err.Error())
	}
	refresh, err := token.NewRefresh()
	if err != nil {
		return nil, envelope.ED(envelope.CodeInternal, "token generation failed", err.Error())
	}
	dev := store.Device{
		ID:            store.NewDeviceID(),
		Name:          sanitizeName(deviceName),
		Created:       now.Unix(),
		LastSeen:      now.Unix(),
		LastIP:        sanitizeIP(clientIP),
		RefreshSHA256: token.HashRefresh(refresh),
		RefreshExp:    now.Unix() + int64(s.Cfg.RefreshTTL),
	}
	if _, err := s.Store.Update(func(st *store.State) error {
		st.Devices = append(st.Devices, dev)
		return nil
	}); err != nil {
		return nil, envelope.ED(envelope.CodeInternal, "state write failed", err.Error())
	}
	audit.Logf("login ok device=%s name=%q", dev.ID, dev.Name)
	return s.issue(secret, &dev, refresh, now)
}

// Refresh 刷新令牌（旋转 + 复用检测，DESIGN §3.1）。
func (s *Service) Refresh(deviceID, refreshToken string) (map[string]any, *envelope.Error) {
	now := s.now()
	secret, err := s.Store.Secret()
	if err != nil {
		return nil, envelope.ED(envelope.CodeInternal, "secret unavailable", err.Error())
	}
	hash := token.HashRefresh(refreshToken)

	var (
		rotated  string // 新 refresh 明文（成功时填充）
		dev      store.Device
		reuseHit bool
	)
	_, err = s.Store.Update(func(st *store.State) error {
		d := st.Find(deviceID)
		if d == nil {
			return errUnknownDevice
		}
		if d.Revoked {
			return errRevoked
		}
		switch {
		case hash == d.RefreshSHA256 && hash != "":
			if now.Unix() > d.RefreshExp {
				return errRefreshExp
			}
			absCap := d.Created + int64(s.Cfg.RefreshAbsTTL)
			newExp := now.Unix() + int64(s.Cfg.RefreshTTL)
			if newExp > absCap {
				newExp = absCap
			}
			if newExp <= now.Unix() {
				return errRefreshExp
			}
			nr, gerr := token.NewRefresh()
			if gerr != nil {
				return gerr
			}
			d.PrevRefreshHash = d.RefreshSHA256
			d.RefreshSHA256 = token.HashRefresh(nr)
			d.RefreshExp = newExp
			d.RotCounter++
			d.LastSeen = now.Unix()
			rotated = nr
			dev = *d
			return nil
		case hash == d.PrevRefreshHash && d.PrevRefreshHash != "":
			// 旧令牌复用 = 泄露信号 → 吊销设备（本变更要落盘：不返回错误）
			d.Revoked = true
			reuseHit = true
			return nil
		default:
			return errBadRefresh
		}
	})
	switch {
	case err == nil && reuseHit:
		audit.Logf("refresh reuse device=%s revoked", deviceID)
		return nil, envelope.E(envelope.CodeDeviceRevoked, "refresh token reuse detected")
	case err == nil:
		audit.Logf("refresh rot device=%s counter=%d", deviceID, dev.RotCounter)
		return s.issue(secret, &dev, rotated, now)
	case errors.Is(err, errRevoked):
		return nil, envelope.E(envelope.CodeDeviceRevoked, "device revoked")
	case errors.Is(err, errRefreshExp):
		return nil, envelope.E(envelope.CodeTokenInvalid, "refresh token expired, re-login required")
	default:
		return nil, envelope.E(envelope.CodeTokenInvalid, "invalid refresh token")
	}
}

// Logout 删除设备记录（refresh 当前/上一枚均可）。
func (s *Service) Logout(deviceID, refreshToken string) (map[string]any, *envelope.Error) {
	hash := token.HashRefresh(refreshToken)
	_, err := s.Store.Update(func(st *store.State) error {
		d := st.Find(deviceID)
		if d == nil {
			return errUnknownDevice
		}
		if hash != d.RefreshSHA256 && hash != d.PrevRefreshHash {
			return errBadRefresh
		}
		out := st.Devices[:0]
		for _, dev := range st.Devices {
			if dev.ID != deviceID {
				out = append(out, dev)
			}
		}
		st.Devices = out
		return nil
	})
	if err != nil {
		return nil, envelope.E(envelope.CodeTokenInvalid, "invalid credentials for logout")
	}
	audit.Logf("logout device=%s", deviceID)
	return map[string]any{}, nil
}

// Authenticate 校验 access_token 并复核设备（吊销即时生效）；顺带节流刷新 last_seen。
func (s *Service) Authenticate(accessToken string) (store.Device, *envelope.Error) {
	now := s.now()
	secret, err := s.Store.Secret()
	if err != nil {
		return store.Device{}, envelope.ED(envelope.CodeInternal, "secret unavailable", err.Error())
	}
	claims, verr := token.Verify(secret, accessToken, now)
	if verr == token.ErrExpired {
		return store.Device{}, envelope.E(envelope.CodeTokenExpired, "access token expired")
	}
	if verr != nil {
		return store.Device{}, envelope.E(envelope.CodeTokenInvalid, "invalid access token")
	}
	st, err := s.Store.Load()
	if err != nil {
		return store.Device{}, envelope.ED(envelope.CodeInternal, "state read failed", err.Error())
	}
	d := st.Find(claims.Sub)
	if d == nil {
		return store.Device{}, envelope.E(envelope.CodeTokenInvalid, "device unknown")
	}
	if d.Revoked {
		return store.Device{}, envelope.E(envelope.CodeDeviceRevoked, "device revoked")
	}
	// last_seen 节流写（省 flash；窗口外的触摸失败不致命）。
	if s.TouchWindow > 0 && now.Unix()-d.LastSeen > s.TouchWindow {
		_, _ = s.Store.Update(func(st *store.State) error {
			if dd := st.Find(claims.Sub); dd != nil {
				dd.LastSeen = now.Unix()
			}
			return nil
		})
		d.LastSeen = now.Unix()
	}
	return *d, nil
}

// ListDevices 已配对设备清单（current 标记当前设备）。
func (s *Service) ListDevices(currentDeviceID string) ([]map[string]any, *envelope.Error) {
	st, err := s.Store.Load()
	if err != nil {
		return nil, envelope.ED(envelope.CodeInternal, "state read failed", err.Error())
	}
	out := make([]map[string]any, 0, len(st.Devices))
	for _, d := range st.Devices {
		out = append(out, map[string]any{
			"device_id": d.ID,
			"name":      d.Name,
			"created":   d.Created,
			"last_seen": d.LastSeen,
			"revoked":   d.Revoked,
			"current":   d.ID == currentDeviceID,
		})
	}
	return out, nil
}

// Revoke 吊销指定设备（含自身；守卫拒绝吊销当前设备之外的未知 id）。
func (s *Service) Revoke(currentDeviceID, targetDeviceID string) *envelope.Error {
	if targetDeviceID == "" {
		return envelope.E(envelope.CodeBadRequest, "device_id required")
	}
	found := false
	_, err := s.Store.Update(func(st *store.State) error {
		d := st.Find(targetDeviceID)
		if d == nil {
			return errUnknownDevice
		}
		d.Revoked = true
		d.RefreshSHA256 = ""
		d.PrevRefreshHash = ""
		found = true
		return nil
	})
	if err != nil || !found {
		return envelope.E(envelope.CodeBadRequest, "device not found")
	}
	audit.Logf("revoke device=%s by=%s", targetDeviceID, currentDeviceID)
	return nil
}

// issue 组装双令牌响应载荷（不含信封——信封由调用层统一包裹，避免
// "部分方法已包、部分未包"的契约不对称）。
func (s *Service) issue(secret []byte, d *store.Device, refresh string, now time.Time) (map[string]any, *envelope.Error) {
	exp := now.Unix() + int64(s.Cfg.AccessTTL)
	access, err := token.Sign(secret, token.Claims{
		Iss: token.Issuer,
		Sub: d.ID,
		Iat: now.Unix(),
		Exp: exp,
		Jti: token.NewJTI(),
	})
	if err != nil {
		return nil, envelope.ED(envelope.CodeInternal, "token generation failed", err.Error())
	}
	return map[string]any{
		"access_token":  access,
		"access_exp":    exp,
		"refresh_token": refresh,
		"refresh_exp":   d.RefreshExp,
		"device_id":     d.ID,
		"router":        s.router(),
	}, nil
}

// sanitizeName 展示名：去控制字符、限 32 字符、空则 "device"。
func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	var b strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
		if b.Len() >= 96 { // 近似 ≤32 个多字节字符
			break
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "device"
	}
	if len(out) > 96 {
		out = out[:96]
	}
	return out
}

// sanitizeIP 仅接受合法 IP（含 IPv6 zone，如 fe80::1%br-lan；展示元数据，
// 防注入进日志/JSON）。
func sanitizeIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" || len(ip) > 64 {
		return ""
	}
	if _, err := netip.ParseAddr(ip); err != nil {
		return ""
	}
	return ip
}

// RootPasswordSet 判断 /etc/shadow 内容中 root 是否已设口令。
//
// 必要性（rpcd 语义，WORKPLAN 坑位 #28）：`/etc/config/rpcd` 的
// `password '$p$root'` 表示"到 shadow 查 root 口令"，而 rpcd 的
// rpc_login_test_password 在**口令为空（未设置）时直接返回 true**——
// 未设口令的机器上 session.login 接受任意密码。WP-5 会签发长效凭据，
// 因此显式要求已设口令，避免"任意密码即可配对"。
func RootPasswordSet(shadowContent string) bool {
	for _, line := range strings.Split(shadowContent, "\n") {
		if !strings.HasPrefix(line, "root:") {
			continue
		}
		rest := line[len("root:"):]
		field := rest
		if i := strings.IndexByte(rest, ':'); i >= 0 {
			field = rest[:i]
		}
		field = strings.TrimSpace(field)
		// 空 / "!" / "*"（及 "!"、"*" 前缀，如 "!*"、"!$6$…"）均为"未设口令"
		return field != "" && field[0] != '!' && field[0] != '*'
	}
	return false
}
