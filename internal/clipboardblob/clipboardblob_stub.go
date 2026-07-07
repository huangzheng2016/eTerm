//go:build !darwin && !linux && !windows

package clipboardblob

func clipboardFilePath() (string, error) {
	return "", ErrNoBlob
}
