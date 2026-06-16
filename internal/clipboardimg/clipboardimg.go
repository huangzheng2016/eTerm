package clipboardimg

import (
	"errors"
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
	return &Image{Data: data, Mime: mime, Filename: "clipboard" + ext}, nil
}
