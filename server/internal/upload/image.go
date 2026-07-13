package upload

import (
	"bytes"
	"fmt"
	"image"
	"net/http"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp" // 注册 webp 解码器（image.RegisterFormat）；imaging.Decode 内部走 image.Decode

	"piaoju/internal/platform/apperr"
)

const (
	maxLongEdge   = 2000       // 契约 §6：长边 >2000 等比缩放到 2000
	thumbLongEdge = 480        // 缩略图长边 480
	jpegQuality   = 80         // 契约 §6：质量 80
	maxPixels     = 40_000_000 // 解码前像素上限（≈40MP）：防解压炸弹（小体积 PNG 声明超大尺寸打爆内存）
)

// processed 处理完成的图片：原图与缩略图均为 JPEG（q80）。
type processed struct {
	orig  []byte // 存储原图（长边 ≤2000）
	thumb []byte // 缩略图（长边 ≤480，不放大）
	w, h  int    // 存储原图的实际尺寸（落库 w/h 真实值）
}

// processImage mime 嗅探（http.DetectContentType，仅 jpeg/png/webp）→ 解码
// → 长边 >2000 等比缩放 → JPEG q80 编码原图 + 480px 缩略图。
// 格式不支持 / 数据损坏（假 mime）一律 41301（契约 §1）。
func processImage(data []byte) (*processed, error) {
	switch mime := http.DetectContentType(data); mime {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return nil, apperr.New(apperr.CodeUploadTooLarge, "unsupported image format, only jpeg/png/webp accepted")
	}
	// 全量解码前先只读头部宽高（DecodeConfig 不分配像素缓冲）：
	// 高压缩比炸弹图（如 30000x30000 纯色 PNG 压缩后 <10MB）解码需 w*h*4 数 GB 内存，必须在解码前拒掉。
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, apperr.New(apperr.CodeUploadTooLarge, "corrupt or unsupported image data")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return nil, apperr.New(apperr.CodeUploadTooLarge, "image dimensions too large")
	}
	// AutoOrientation：JPEG 按 EXIF 方向摆正后再存，避免旋转图（png/webp 无 EXIF，无副作用）。
	img, err := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	if err != nil {
		// 魔数伪装成图片但实际解不开（假 mime）也归到 41301。
		return nil, apperr.New(apperr.CodeUploadTooLarge, "corrupt or unsupported image data")
	}

	var orig image.Image = img
	if b := img.Bounds(); b.Dx() > maxLongEdge || b.Dy() > maxLongEdge {
		orig = imaging.Fit(img, maxLongEdge, maxLongEdge, imaging.Lanczos)
	}
	thumb := imaging.Fit(img, thumbLongEdge, thumbLongEdge, imaging.Lanczos) // Fit 不放大小图

	origBytes, err := encodeJPEG(orig)
	if err != nil {
		return nil, fmt.Errorf("upload: encode original: %w", err)
	}
	thumbBytes, err := encodeJPEG(thumb)
	if err != nil {
		return nil, fmt.Errorf("upload: encode thumbnail: %w", err)
	}
	ob := orig.Bounds()
	return &processed{orig: origBytes, thumb: thumbBytes, w: ob.Dx(), h: ob.Dy()}, nil
}

func encodeJPEG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, img, imaging.JPEG, imaging.JPEGQuality(jpegQuality)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
