package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	h, err := hashPassword("s3cret-password")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$") {
		t.Fatalf("hash = %q, want PHC argon2id prefix", h)
	}
	if len(h) > 255 {
		t.Fatalf("hash len = %d, exceeds users.password_hash VARCHAR(255)", len(h))
	}
	if !verifyPassword(h, "s3cret-password") {
		t.Fatal("correct password must verify")
	}
	if verifyPassword(h, "wrong-password") {
		t.Fatal("wrong password must not verify")
	}
}

// TestHashPasswordSaltsDiffer 同口令两次哈希必须不同（随机盐）。
func TestHashPasswordSaltsDiffer(t *testing.T) {
	h1, err1 := hashPassword("same-password")
	h2, err2 := hashPassword("same-password")
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	if h1 == h2 {
		t.Fatal("two hashes of the same password must differ (random salt)")
	}
	if !verifyPassword(h1, "same-password") || !verifyPassword(h2, "same-password") {
		t.Fatal("both hashes must verify")
	}
}

// TestVerifyPasswordMalformed 各类损坏哈希一律 false，不 panic。
func TestVerifyPasswordMalformed(t *testing.T) {
	bad := []string{
		"",
		"plaintext",
		"$argon2i$v=19$m=19456,t=2,p=1$AAAA$BBBB",     // 错误变体
		"$argon2id$v=18$m=19456,t=2,p=1$AAAA$BBBB",    // 错误版本
		"$argon2id$v=19$m=abc,t=2,p=1$AAAA$BBBB",      // 参数非数字
		"$argon2id$v=19$m=19456,t=2,p=1$!!!$BBBB",     // salt 非 base64
		"$argon2id$v=19$m=19456,t=2,p=1$AAAA$!!!",     // key 非 base64
		"$argon2id$v=19$m=99999999,t=2,p=1$AAAA$QUFB", // 超上限参数（防 DoS）
	}
	for _, h := range bad {
		if verifyPassword(h, "whatever") {
			t.Fatalf("malformed hash %q must not verify", h)
		}
	}
}
