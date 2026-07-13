package auth

// argon2id 密码哈希（conventions §3），PHC 字符串格式落库：
//
//	$argon2id$v=19$m=19456,t=2,p=1$<b64 salt>$<b64 key>
//
// 参数随哈希存储，未来调参后旧哈希仍可校验。参数取 OWASP 推荐档
// （19 MiB / t=2 / p=1），单次 ~15ms，适合登录路径。

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemoryKiB = 19 * 1024
	argonTime      = 2
	argonThreads   = 1
	argonSaltLen   = 16
	argonKeyLen    = 32
)

// 解析外部哈希时的参数上限（防被篡改的超大参数打挂服务）。
const (
	argonMaxMemoryKiB = 1 << 20 // 1 GiB
	argonMaxTime      = 16
	argonMaxThreads   = 8
	argonMaxKeyLen    = 128
)

// hashPassword 生成随机盐并输出 PHC 格式 argon2id 哈希。
func hashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// verifyPassword 按 encoded 内嵌参数重算并常数时间比较；
// 任何解析失败一律返回 false（调用方统一回 40103，不泄漏原因）。
func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var (
		m, t uint32
		p    uint8
	)
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	if m == 0 || m > argonMaxMemoryKiB || t == 0 || t > argonMaxTime || p == 0 || p > argonMaxThreads {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 || len(want) > argonMaxKeyLen {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

var (
	dummyOnce sync.Once
	dummyHash string
)

// dummyVerify 邮箱不存在时也做一次等成本 argon2 校验，
// 压平「未注册 vs 密码错误」的响应时间差（防账号枚举的时间侧信道）。
func dummyVerify(password string) {
	dummyOnce.Do(func() {
		dummyHash, _ = hashPassword("piaoju-dummy-password-for-timing")
	})
	verifyPassword(dummyHash, password)
}
