package upload

// processImage 单测（S4 DoD：HEIC/非图片拒收、压缩缩略图链路）。
// 样张用 webp_gen_test.go 的 genWebP（真实解码路径，无需二进制 fixture）。

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"testing"

	"piaoju/internal/platform/apperr"
)

func wantTooLarge(t *testing.T, err error) {
	t.Helper()
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != apperr.CodeUploadTooLarge {
		t.Fatalf("err = %v, want apperr %d", err, apperr.CodeUploadTooLarge)
	}
}

func TestProcessImageResizesLongEdge(t *testing.T) {
	p, err := processImage(genWebP(2500, 1000, 200, 120, 40, 255))
	if err != nil {
		t.Fatalf("processImage: %v", err)
	}
	if p.w != 2000 || p.h != 800 {
		t.Fatalf("stored size = %dx%d, want 2000x800 (long edge capped)", p.w, p.h)
	}
	orig, _, err := image.Decode(bytes.NewReader(p.orig))
	if err != nil {
		t.Fatalf("decode stored original: %v", err)
	}
	if b := orig.Bounds(); b.Dx() != 2000 || b.Dy() != 800 {
		t.Fatalf("original jpeg = %dx%d, want 2000x800", b.Dx(), b.Dy())
	}
	thumb, _, err := image.Decode(bytes.NewReader(p.thumb))
	if err != nil {
		t.Fatalf("decode thumbnail: %v", err)
	}
	if b := thumb.Bounds(); b.Dx() > thumbLongEdge || b.Dy() > thumbLongEdge {
		t.Fatalf("thumbnail = %dx%d, want long edge <= %d", b.Dx(), b.Dy(), thumbLongEdge)
	}
}

func TestProcessImageSmallNotUpscaled(t *testing.T) {
	p, err := processImage(genWebP(100, 50, 10, 20, 30, 255))
	if err != nil {
		t.Fatalf("processImage: %v", err)
	}
	if p.w != 100 || p.h != 50 {
		t.Fatalf("stored size = %dx%d, want 100x50 unchanged", p.w, p.h)
	}
	thumb, _, err := image.Decode(bytes.NewReader(p.thumb))
	if err != nil {
		t.Fatalf("decode thumbnail: %v", err)
	}
	if b := thumb.Bounds(); b.Dx() != 100 || b.Dy() != 50 {
		t.Fatalf("thumbnail = %dx%d, want 100x50 (Fit 不放大)", b.Dx(), b.Dy())
	}
}

func TestProcessImageRejectsNonImage(t *testing.T) {
	_, err := processImage([]byte("这不是图片，是一段纯文本内容 plain text body"))
	wantTooLarge(t, err)
}

// HEIC：iPhone 默认格式，服务端不支持 → 41301（契约 §6 仅 jpeg/png/webp）。
func TestProcessImageRejectsHEIC(t *testing.T) {
	heic := append([]byte{0, 0, 0, 0x18}, []byte("ftypheic")...)
	heic = append(heic, bytes.Repeat([]byte{0}, 64)...)
	_, err := processImage(heic)
	wantTooLarge(t, err)
}

// 魔数伪装成 JPEG 但实际解不开（假 mime）→ 同样 41301。
func TestProcessImageRejectsCorruptJPEG(t *testing.T) {
	fake := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{0xAB}, 128)...)
	_, err := processImage(fake)
	wantTooLarge(t, err)
}

// pngWithHeader 构造带合法签名 + IHDR 的 PNG 头（无像素数据）。
// DecodeConfig 只读 IHDR，足以触发解码前的尺寸校验。
func pngWithHeader(w, h uint32) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8] = 8 // bit depth
	ihdr[9] = 2 // color type: truecolor
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'})
	binary.Write(&buf, binary.BigEndian, uint32(len(ihdr)))
	buf.WriteString("IHDR")
	buf.Write(ihdr)
	binary.Write(&buf, binary.BigEndian, crc32.ChecksumIEEE(append([]byte("IHDR"), ihdr...)))
	return buf.Bytes()
}

// 解压炸弹：小体积 PNG 声明 30000x30000（解码需 ~3.6GB RGBA）→ 必须在全量解码前拒（41301）。
// 若该防线失效，此测试会尝试真解码并 OOM/超时，而不是静默通过。
func TestProcessImageRejectsDecompressionBomb(t *testing.T) {
	_, err := processImage(pngWithHeader(30000, 30000))
	wantTooLarge(t, err)
}

// 阈值边界：恰好 40MP 不被尺寸校验拦（走到解码阶段），超 1 行即被尺寸校验拒。
func TestProcessImagePixelCapBoundary(t *testing.T) {
	wantMsg := func(err error, msg string) {
		t.Helper()
		var ae *apperr.Error
		if !errors.As(err, &ae) || ae.Code != apperr.CodeUploadTooLarge || ae.Msg != msg {
			t.Fatalf("err = %v, want apperr %d %q", err, apperr.CodeUploadTooLarge, msg)
		}
	}
	// 8000x5000 = 40MP，恰好等于 maxPixels：通过尺寸校验，因无 IDAT 在全量解码阶段报 corrupt。
	_, err := processImage(pngWithHeader(8000, 5000))
	wantMsg(err, "corrupt or unsupported image data")
	// 8000x5001 > maxPixels：尺寸校验直接拒。
	_, err = processImage(pngWithHeader(8000, 5001))
	wantMsg(err, "image dimensions too large")
}
