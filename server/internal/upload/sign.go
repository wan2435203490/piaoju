// Package upload 契约 §6：图片上传（压缩+缩略图+落库）与带签名的静态文件服务。
//
//   - POST /api/v1/uploads（Routes，主线程挂 Auth 组）
//   - GET  /uploads/{path}（Serve，主线程挂根路由，不带 Auth，靠签名 URL 防越权）
package upload

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"
)

// URLTTL 签名 URL 有效期（契约 §6：7 天）。ticket 模块生成附件 URL 时复用。
const URLTTL = 7 * 24 * time.Hour

// SignURL 为静态文件路径生成带过期签名的 URL。
// path 形如 /uploads/<uid>/<file>.jpg；返回 path?exp=<unix秒>&sig=<hex(hmac-sha256(secret, path+exp))>。
// 供 ticket 模块 import 生成附件 url/thumbUrl（依赖方向 ticket→upload，任务卡允许）。
func SignURL(secret, path string, ttl time.Duration) string {
	exp := time.Now().Add(ttl).Unix()
	return path + "?exp=" + strconv.FormatInt(exp, 10) + "&sig=" + signature(secret, path, exp)
}

// signature = hex(hmac-sha256(secret, path+exp))。
func signature(secret, path string, exp int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(path + strconv.FormatInt(exp, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

// verifySignedPath 校验 exp 未过期且 sig 匹配。
// sig 先 hex 解码再用 hmac.Equal 恒时比较；任何失败不区分原因（防探测，统一 40401）。
func verifySignedPath(secret, path, expStr, sigHex string, now time.Time) bool {
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || now.Unix() > exp {
		return false
	}
	got, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(signature(secret, path, exp))
	if err != nil {
		return false
	}
	return hmac.Equal(want, got)
}
