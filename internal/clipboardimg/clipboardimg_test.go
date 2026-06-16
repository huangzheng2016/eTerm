package clipboardimg

import "testing"

func png1x1() []byte {
	return []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, '\r', 'I', 'H', 'D', 'R', 0, 0, 0, 1, 0, 0, 0, 1, 8, 2, 0, 0, 0, 0x90, 0x77, 0x53, 0xde}
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
