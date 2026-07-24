package clipboardimg

import (
	"bytes"
	"errors"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"net/http"
)

const MaxImageBytes = 10 * 1024 * 1024

var ErrNoImage = errors.New("clipboard does not contain a supported image")
var ErrImageTooLarge = errors.New("clipboard image exceeds 10 MiB")

type Image struct {
	Data     []byte
	Mime     string
	Filename string
}

func sniffMime(data []byte) string {
	mime := http.DetectContentType(data)
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return mime
	default:
		return ""
	}
}

func validate(data []byte) (*Image, error) {
	if len(data) == 0 {
		return nil, ErrNoImage
	}
	if len(data) > MaxImageBytes {
		return nil, ErrImageTooLarge
	}
	mime := sniffMime(data)
	if mime == "" {
		return nil, ErrNoImage
	}
	ext := ".png"
	switch mime {
	case "image/jpeg":
		ext = ".jpg"
	case "image/gif":
		ext = ".gif"
	case "image/webp":
		ext = ".webp"
	}
	if mime == "image/png" || mime == "image/jpeg" {
		if jpg := CompressJPEG(data); jpg != nil {
			return &Image{Data: jpg, Mime: "image/jpeg", Filename: "clipboard.jpg"}, nil
		}
	}
	return &Image{Data: data, Mime: mime, Filename: "clipboard" + ext}, nil
}

// CompressJPEG re-encodes a PNG/JPEG as JPEG quality 70, flattened onto
// white. Returns nil when decoding fails or the result is not smaller.
func CompressJPEG(data []byte) []byte {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	b := src.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, image.White, image.Point{}, draw.Src)
	draw.Draw(dst, b, src, b.Min, draw.Over)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 70}); err != nil {
		return nil
	}
	if buf.Len() >= len(data) {
		return nil
	}
	return buf.Bytes()
}
