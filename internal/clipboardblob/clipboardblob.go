package clipboardblob

import (
	"errors"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/huangzheng2016/eTerm/internal/clipboardimg"
)

const MaxBytes = 10 * 1024 * 1024

var ErrNoBlob = errors.New("clipboard does not contain a supported file or image")
var ErrBlobTooLarge = errors.New("clipboard blob exceeds 10 MiB")

var readClipboardFilePath = clipboardFilePath
var readClipboardImage = clipboardimg.Read

type Blob struct {
	Data      []byte
	Mime      string
	Filename  string
	LocalPath string
}

func Read() (*Blob, error) {
	path, err := readClipboardFilePath()
	if err == nil {
		blob, fileErr := fromFilePath(path)
		if fileErr == nil {
			return blob, nil
		}
		if fileErr != ErrNoBlob {
			return nil, fileErr
		}
	} else if err != ErrNoBlob {
		return nil, err
	}
	img, err := readClipboardImage()
	if err == clipboardimg.ErrNoImage {
		return nil, ErrNoBlob
	}
	if err == clipboardimg.ErrImageTooLarge {
		return nil, ErrBlobTooLarge
	}
	if err != nil {
		return nil, err
	}
	return &Blob{Data: img.Data, Mime: img.Mime, Filename: img.Filename}, nil
}

func fromFilePath(path string) (*Blob, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, ErrNoBlob
	}
	if st.IsDir() {
		return &Blob{Filename: filepath.Base(path), LocalPath: path}, nil
	}
	if st.Size() > MaxBytes {
		return nil, ErrBlobTooLarge
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	mt := fileMime(path, data)
	filename := filepath.Base(path)
	if mt == "image/png" || mt == "image/jpeg" {
		if jpg := clipboardimg.CompressJPEG(data); jpg != nil {
			data = jpg
			mt = "image/jpeg"
			filename = strings.TrimSuffix(filename, filepath.Ext(filename)) + ".jpg"
		}
	}
	return &Blob{
		Data:      data,
		Mime:      mt,
		Filename:  filename,
		LocalPath: path,
	}, nil
}

func fileMime(path string, data []byte) string {
	if ext := filepath.Ext(path); ext != "" {
		if mt := mime.TypeByExtension(ext); mt != "" {
			if i := strings.IndexByte(mt, ';'); i >= 0 {
				mt = mt[:i]
			}
			return strings.TrimSpace(mt)
		}
	}
	if len(data) > 0 {
		return http.DetectContentType(data)
	}
	return "application/octet-stream"
}

func filePathFromURIList(data string) (string, error) {
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		u, err := url.Parse(line)
		if err != nil || u.Scheme != "file" || u.Path == "" {
			continue
		}
		return u.Path, nil
	}
	return "", ErrNoBlob
}
