//go:build darwin

package clipboardblob

import (
	"context"
	"strings"
	"time"

	"golang.design/x/clipboard"
)

func clipboardFilePath() (string, error) {
	if err := clipboard.Init(); err != nil {
		return "", ErrNoBlob
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	raw, err := clipboard.Read(ctx, clipboard.Register("public.file-url"))
	if err != nil {
		return "", ErrNoBlob
	}
	data := strings.TrimRight(string(raw), "\x00")
	if data == "" {
		return "", ErrNoBlob
	}
	return filePathFromURIList(data)
}
