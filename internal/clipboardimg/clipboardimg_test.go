package clipboardimg

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func png1x1() []byte {
	return []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, '\r', 'I', 'H', 'D', 'R', 0, 0, 0, 1, 0, 0, 0, 1, 8, 2, 0, 0, 0, 0x90, 0x77, 0x53, 0xde}
}

func noisyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * y), G: uint8(x), B: uint8(y), A: 200})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestValidateRejectsEmpty(t *testing.T) {
	if _, err := validate(nil); err != ErrNoImage {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateRejectsTooLarge(t *testing.T) {
	data := make([]byte, MaxImageBytes+1)
	if _, err := validate(data); err != ErrImageTooLarge {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateAcceptsPNG(t *testing.T) {
	img, err := validate(png1x1())
	if err != nil {
		t.Fatal(err)
	}
	if img.Mime != "image/png" || img.Filename != "clipboard.png" {
		t.Fatalf("got %#v", img)
	}
}

func TestValidateCompressesPNGToJPEG(t *testing.T) {
	data := noisyPNG(t)
	img, err := validate(data)
	if err != nil {
		t.Fatal(err)
	}
	if img.Mime != "image/jpeg" || img.Filename != "clipboard.jpg" {
		t.Fatalf("got %#v", img)
	}
	if len(img.Data) >= len(data) {
		t.Fatalf("jpeg %d bytes >= png %d bytes", len(img.Data), len(data))
	}
}

func TestCompressJPEGKeepsSmallerOriginal(t *testing.T) {
	if CompressJPEG(png1x1()) != nil {
		t.Fatal("expected nil for tiny png")
	}
}
