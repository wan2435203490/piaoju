package token

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestSignVerifyRoundTrip(t *testing.T) {
	tm := NewManager("test-secret-32-bytes-minimum!!!!")
	s, err := tm.Sign(42, time.Minute)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	uid, err := tm.Verify(s)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if uid != 42 {
		t.Fatalf("uid = %d, want 42", uid)
	}
}

func TestVerifyExpired(t *testing.T) {
	tm := NewManager("secret")
	s, err := tm.Sign(7, -time.Minute) // 已过期
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if _, err := tm.Verify(s); !errors.Is(err, ErrExpired) {
		t.Fatalf("err = %v, want ErrExpired", err)
	}
}

func TestVerifyWrongSecret(t *testing.T) {
	s, err := NewManager("secret-a").Sign(1, time.Minute)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if _, err := NewManager("secret-b").Verify(s); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestVerifyGarbage(t *testing.T) {
	tm := NewManager("secret")
	for _, s := range []string{"", "not-a-jwt", "a.b.c"} {
		if _, err := tm.Verify(s); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Verify(%q) err = %v, want ErrInvalid", s, err)
		}
	}
}

// TestVerifyRejectsNoneAlg alg=none 伪造 token 必须被拒（WithValidMethods 白名单）。
func TestVerifyRejectsNoneAlg(t *testing.T) {
	claims := jwt.RegisteredClaims{
		Subject:   "42",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	forged, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("forge: %v", err)
	}
	if _, err := NewManager("secret").Verify(forged); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid for alg=none", err)
	}
}

// TestVerifyBadSubject subject 非正整数（非本系统签发的语义）必须拒。
func TestVerifyBadSubject(t *testing.T) {
	tm := NewManager("secret")
	now := time.Now()
	for _, sub := range []string{"", "abc", "-5", "0"} {
		claims := jwt.RegisteredClaims{
			Subject:   sub,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		}
		s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("secret"))
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		if _, err := tm.Verify(s); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Verify(sub=%q) err = %v, want ErrInvalid", sub, err)
		}
	}
}
