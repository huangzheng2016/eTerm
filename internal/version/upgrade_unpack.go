package version

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func extractArchive(archivePath, destDir, innerName string) error {
	switch {
	case strings.HasSuffix(archivePath, ".zip"):
		return extractZipSingle(archivePath, destDir, innerName)
	case strings.HasSuffix(archivePath, ".tar.gz"):
		return extractTarGzSingle(archivePath, destDir, innerName)
	default:
		return fmt.Errorf("unknown archive layout")
	}
}

func extractTarGzSingle(archivePath, destDir, wantBase string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		base := filepath.Base(hdr.Name)
		if base != wantBase {
			continue
		}
		outPath := filepath.Join(destDir, wantBase)
		out, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("%s not found in archive", wantBase)
}

func extractZipSingle(archivePath, destDir, wantBase string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()

	for _, ze := range zr.File {
		if ze.Mode().IsDir() {
			continue
		}
		base := filepath.Base(ze.Name)
		if base != wantBase {
			continue
		}
		rc, err := ze.Open()
		if err != nil {
			return err
		}
		outPath := filepath.Join(destDir, wantBase)
		out, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			rc.Close()
			return err
		}
		_, cerr := io.Copy(out, rc)
		rc.Close()
		cerr2 := out.Close()
		if cerr != nil {
			return cerr
		}
		if cerr2 != nil {
			return cerr2
		}
		return nil
	}
	return fmt.Errorf("%s not found in zip", wantBase)
}
