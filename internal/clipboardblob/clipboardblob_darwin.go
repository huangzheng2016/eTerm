//go:build darwin

package clipboardblob

import (
	"strings"

	"golang.design/x/clipboard"
)

func clipboardFilePath() (string, error) {
	if err := clipboard.Init(); err != nil {
		return "", ErrNoBlob
	}
	data := strings.TrimRight(string(clipboard.Read(clipboard.Register("public.file-url"))), "\x00")
	if data == "" {
		return "", ErrNoBlob
	}
	return filePathFromURIList(data)
}
