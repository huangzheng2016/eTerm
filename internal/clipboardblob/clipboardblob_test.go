package clipboardblob

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/huangzheng2016/eTerm/internal/clipboardimg"
)

func TestFromFilePathUsesFileBytesNameAndMime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archive.zip")
	data := []byte("PK\x03\x04zip-data")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	blob, err := fromFilePath(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(blob.Data) != string(data) {
		t.Fatalf("data = %q", blob.Data)
	}
	if blob.Filename != "archive.zip" {
		t.Fatalf("filename = %q", blob.Filename)
	}
	if blob.Mime != "application/zip" {
		t.Fatalf("mime = %q", blob.Mime)
	}
	if blob.LocalPath != path {
		t.Fatalf("local path = %q", blob.LocalPath)
	}
}

func TestFromFilePathRejectsTooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.bin")
	if err := os.WriteFile(path, make([]byte, MaxBytes+1), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := fromFilePath(path)
	if err != ErrBlobTooLarge {
		t.Fatalf("err = %v", err)
	}
}

func TestFromFilePathCompressesPNG(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * y), G: uint8(x), B: uint8(y), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	blob, err := fromFilePath(path)
	if err != nil {
		t.Fatal(err)
	}
	if blob.Mime != "image/jpeg" || blob.Filename != "shot.jpg" {
		t.Fatalf("got mime=%q filename=%q", blob.Mime, blob.Filename)
	}
	if len(blob.Data) >= buf.Len() {
		t.Fatalf("jpeg %d bytes >= png %d bytes", len(blob.Data), buf.Len())
	}
	if blob.LocalPath != path {
		t.Fatalf("local path = %q", blob.LocalPath)
	}
}

func TestFromFilePathReturnsDirWithoutData(t *testing.T) {
	dir := t.TempDir()
	blob, err := fromFilePath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(blob.Data) != 0 {
		t.Fatalf("data = %d bytes", len(blob.Data))
	}
	if blob.LocalPath != dir {
		t.Fatalf("local path = %q", blob.LocalPath)
	}
	if blob.Filename != filepath.Base(dir) {
		t.Fatalf("filename = %q", blob.Filename)
	}
}

func TestReadIgnoresMissingClipboardFilePath(t *testing.T) {
	oldPath := readClipboardFilePath
	oldImage := readClipboardImage
	readClipboardFilePath = func() (string, error) {
		return filepath.Join(t.TempDir(), "missing.zip"), nil
	}
	readClipboardImage = func() (*clipboardimg.Image, error) {
		return nil, clipboardimg.ErrNoImage
	}
	t.Cleanup(func() {
		readClipboardFilePath = oldPath
		readClipboardImage = oldImage
	})

	_, err := Read()
	if err != ErrNoBlob {
		t.Fatalf("err = %v", err)
	}
}

func TestFilePathFromURIList(t *testing.T) {
	path, err := filePathFromURIList("# copied file\nfile:///tmp/a%20b.tar.gz\n")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/tmp/a b.tar.gz" {
		t.Fatalf("path = %q", path)
	}
}
