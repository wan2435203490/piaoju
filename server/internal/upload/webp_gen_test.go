package upload

import (
	"bytes"
	"encoding/binary"
	"image"
	"testing"
)

// x/image/webp 只有解码器（纯 Go 无 webp encoder），无法用标准库编码 3000×2000 样张。
// 这里手写最小 VP8L（lossless）位流：纯色图 + 单符号 simple huffman，
// 每像素 0 bit，任意尺寸都是 ~30 字节，供真实解码+压缩链路测试。

// bitWriter VP8L LSB-first 位流。
type bitWriter struct {
	buf []byte
	n   uint
}

func (w *bitWriter) writeBits(v uint32, bits uint) {
	for i := uint(0); i < bits; i++ {
		if w.n%8 == 0 {
			w.buf = append(w.buf, 0)
		}
		if v>>i&1 == 1 {
			w.buf[len(w.buf)-1] |= 1 << (w.n % 8)
		}
		w.n++
	}
}

// genWebP 生成 w×h 纯色 (r,g,b,a) 的合法 lossless WebP。
func genWebP(w, h int, r, g, b, a uint8) []byte {
	bw := &bitWriter{}
	bw.writeBits(0x2f, 8)                  // VP8L magic
	bw.writeBits(uint32(w-1), 14)          // width-1
	bw.writeBits(uint32(h-1), 14)          // height-1
	bw.writeBits(0, 1)                     // alpha hint
	bw.writeBits(0, 3)                     // version 0
	bw.writeBits(0, 1)                     // 无 transform
	bw.writeBits(0, 1)                     // 无 color cache
	bw.writeBits(0, 1)                     // 无 meta huffman
	tree := func(sym uint32, eight bool) { // simple code、单符号（读取 0 bit/符号）
		bw.writeBits(1, 1) // simple
		bw.writeBits(0, 1) // num_symbols-1 = 0
		if eight {
			bw.writeBits(1, 1) // 8-bit 符号
			bw.writeBits(sym, 8)
		} else {
			bw.writeBits(0, 1) // 1-bit 符号
			bw.writeBits(sym, 1)
		}
	}
	tree(uint32(g), true) // green
	tree(uint32(r), true) // red
	tree(uint32(b), true) // blue
	tree(uint32(a), true) // alpha
	tree(0, false)        // distance
	payload := bw.buf

	var out bytes.Buffer
	out.WriteString("RIFF")
	chunkLen := len(payload)
	padded := chunkLen + chunkLen%2
	binary.Write(&out, binary.LittleEndian, uint32(4+8+padded)) //nolint:errcheck
	out.WriteString("WEBPVP8L")
	binary.Write(&out, binary.LittleEndian, uint32(chunkLen)) //nolint:errcheck
	out.Write(payload)
	if chunkLen%2 == 1 {
		out.WriteByte(0)
	}
	return out.Bytes()
}

// TestGenWebPDecodes 自检：生成物必须能被 x/image/webp 解码且尺寸/颜色正确。
func TestGenWebPDecodes(t *testing.T) {
	data := genWebP(3000, 2000, 200, 120, 40, 255)
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode generated webp: %v", err)
	}
	if format != "webp" {
		t.Fatalf("format = %q, want webp", format)
	}
	if b := img.Bounds(); b.Dx() != 3000 || b.Dy() != 2000 {
		t.Fatalf("bounds = %dx%d, want 3000x2000", b.Dx(), b.Dy())
	}
	r, g, b, _ := img.At(1500, 1000).RGBA()
	if r>>8 != 200 || g>>8 != 120 || b>>8 != 40 {
		t.Fatalf("pixel = (%d,%d,%d), want (200,120,40)", r>>8, g>>8, b>>8)
	}
}
