package clipboardblob

import (
	"os"
	"path/filepath"
	"testing"
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

func TestFilePathFromURIList(t *testing.T) {
	path, err := filePathFromURIList("# copied file\nfile:///tmp/a%20b.tar.gz\n")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/tmp/a b.tar.gz" {
		t.Fatalf("path = %q", path)
	}
}
