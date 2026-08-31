package main

import (
	"archive/tar"
	"compress/bzip2"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	senseVoiceURL = "https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17.tar.bz2"
	senseVoiceDir = "sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17"
	sileroVadURL  = "https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/silero_vad.onnx"
)

func defaultModelRoot() string {
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = os.TempDir()
	}
	return filepath.Join(cache, "eterm", "voice-models")
}

// asrModelPaths returns (model, tokens) for a SenseVoice model directory,
// preferring fp32 model.onnx over model.int8.onnx.
func asrModelPaths(dir string) (string, string, error) {
	tokens := filepath.Join(dir, "tokens.txt")
	if _, err := os.Stat(tokens); err != nil {
		return "", "", fmt.Errorf("tokens.txt not found in %s", dir)
	}
	for _, name := range []string{"model.onnx", "model.int8.onnx"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p, tokens, nil
		}
	}
	return "", "", fmt.Errorf("no model.onnx or model.int8.onnx in %s", dir)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// ensureModels downloads the default ASR model and VAD model into root on
// first use, reporting progress through ev. Returns (asrDir, vadModel).
func ensureModels(ctx context.Context, root string, ev *eventWriter) (string, string, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", "", err
	}

	vadPath := filepath.Join(root, "silero_vad.onnx")
	if !fileExists(vadPath) {
		ev.errorf("downloading silero_vad.onnx")
		if err := downloadTo(ctx, sileroVadURL, vadPath, ev); err != nil {
			return "", "", fmt.Errorf("download VAD model: %w", err)
		}
	}

	asrDir := filepath.Join(root, senseVoiceDir)
	if _, _, err := asrModelPaths(asrDir); err != nil {
		archive := filepath.Join(root, senseVoiceDir+".tar.bz2")
		if !fileExists(archive) {
			ev.errorf("downloading SenseVoice model (about 700 MB, one time)")
			if err := downloadTo(ctx, senseVoiceURL, archive, ev); err != nil {
				return "", "", fmt.Errorf("download ASR model: %w", err)
			}
		}
		if err := untarBz2(archive, root); err != nil {
			return "", "", fmt.Errorf("extract ASR model: %w", err)
		}
		os.Remove(archive)
		if _, _, err := asrModelPaths(asrDir); err != nil {
			return "", "", err
		}
	}

	return asrDir, vadPath, nil
}

func downloadTo(ctx context.Context, url, dest string, ev *eventWriter) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}

	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer f.Close()

	total := resp.ContentLength
	var got int64
	buf := make([]byte, 256*1024)
	lastPct := -1.0
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				os.Remove(tmp)
				return werr
			}
			got += int64(n)
			if total > 0 {
				pct := float64(got) / float64(total) * 100
				if pct-lastPct >= 1 || pct >= 100 {
					lastPct = pct
					ev.progress(pct)
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			os.Remove(tmp)
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}

func untarBz2(archive, destDir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()

	tr := tar.NewReader(bzip2.NewReader(f))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(hdr.Name)
		if strings.Contains(name, "..") {
			return fmt.Errorf("unsafe path in archive: %s", hdr.Name)
		}
		target := filepath.Join(destDir, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0o755)
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
		}
	}
}
