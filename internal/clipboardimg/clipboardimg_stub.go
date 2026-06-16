//go:build !darwin && !linux && !windows

package clipboardimg

func Read() (*Image, error) {
	return nil, ErrNoImage
}
