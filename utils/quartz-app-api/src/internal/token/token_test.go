package token

import (
	"strings"
	"testing"
	"time"
)

func TestSignVerifyRoundTrip(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	now := time.Unix(1760000000, 0)
	tok, err := Sign(secret, Claims{Iss: Issuer, Sub: "dev1", Iat: now.Unix(), Exp: now.Add(15 * time.Minute).Unix(), Jti: NewJTI()})
	if err != nil {
		t.Fatal(err)
	}
	c, err := Verify(secret, tok, now)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if c.Sub != "dev1" {
		t.Fatalf("sub=%q", c.Sub)
	}
}

func TestVerifyExpired(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	now := time.Unix(1760000000, 0)
	tok, _ := Sign(secret, Claims{Iss: Issuer, Sub: "dev1", Iat: now.Unix(), Exp: now.Add(time.Minute).Unix()})
	// 过期未超 skew：仍可用
	if _, err := Verify(secret, tok, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("within skew should pass: %v", err)
	}
	// 过期超 skew：ErrExpired
	if _, err := Verify(secret, tok, now.Add(10*time.Minute)); err != ErrExpired {
		t.Fatalf("want ErrExpired, got %v", err)
	}
	// 时钟回拨（iat 在未来超 skew）：ErrInvalid
	if _, err := Verify(secret, tok, now.Add(-10*time.Minute)); err != ErrInvalid {
		t.Fatalf("want ErrInvalid for future iat, got %v", err)
	}
}

func TestVerifyTamper(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	now := time.Unix(1760000000, 0)
	tok, _ := Sign(secret, Claims{Iss: Issuer, Sub: "dev1", Iat: now.Unix(), Exp: now.Add(time.Hour).Unix()})
	parts := strings.Split(tok, ".")

	// 篡改 payload
	fake := parts[0] + "." + parts[1][:len(parts[1])-2] + "AA" + "." + parts[2]
	if _, err := Verify(secret, fake, now); err != ErrInvalid {
		t.Fatalf("tampered payload: %v", err)
	}
	// 错误密钥
	if _, err := Verify([]byte("another-key-another-key-another"), tok, now); err != ErrInvalid {
		t.Fatalf("wrong key: %v", err)
	}
	// 结构错误
	for _, bad := range []string{"", "a.b", "a.b.c.d", "!!!.@.@"} {
		if _, err := Verify(secret, bad, now); err != ErrInvalid {
			t.Fatalf("malformed %q: %v", bad, err)
		}
	}
}

func TestHashRefreshStable(t *testing.T) {
	h1 := HashRefresh("tok")
	h2 := HashRefresh("tok")
	if h1 != h2 || len(h1) != 64 {
		t.Fatalf("hash mismatch: %s vs %s", h1, h2)
	}
	if HashRefresh("tok2") == h1 {
		t.Fatal("distinct tokens must hash differently")
	}
}
