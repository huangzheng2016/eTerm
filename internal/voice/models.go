package voice

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

const (
	ModelKindSenseVoice     = "sensevoice"
	ModelKindSenseVoiceInt8 = "sensevoice-int8"
	ModelKindParaformer     = "paraformer"
)

type ModelSpec struct {
	ID   string
	Name string
	Kind string
	Dir  string
	File string
	URL  string
	Size string
}

const senseVoice20240717 = "sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17"

var modelCatalog = []ModelSpec{
	{
		ID: "sensevoice", Name: "SenseVoice 2024-07-17", Kind: ModelKindSenseVoice,
		Dir: senseVoice20240717, File: "model.onnx",
		URL:  "https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/" + senseVoice20240717 + ".tar.bz2",
		Size: "1.0 GB",
	},
	{
		ID: "paraformer-zh-small", Name: "Paraformer zh-small int8", Kind: ModelKindParaformer,
		Dir: "sherpa-onnx-paraformer-zh-small-2024-03-09", File: "model.int8.onnx",
		URL:  "https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/sherpa-onnx-paraformer-zh-small-2024-03-09.tar.bz2",
		Size: "74 MB",
	},
}

func ModelCatalog() []ModelSpec { return modelCatalog }

func ModelByID(id string) ModelSpec {
	for _, m := range modelCatalog {
		if m.ID == id {
			return m
		}
	}
	return modelCatalog[0]
}

func LegacyModelID(id string) (newID string, int8 bool, legacy bool) {
	switch id {
	case "sensevoice-fp32":
		return modelCatalog[0].ID, false, true
	case "sensevoice-int8":
		return modelCatalog[0].ID, true, true
	}
	return "", false, false
}

func ModelsRoot() string {
	return filepath.Join(DefaultCacheDir(), "voice-models")
}

func (m ModelSpec) ModelDir(root string) string {
	return filepath.Join(root, m.Dir)
}

func (m ModelSpec) Installed(root string) bool {
	dir := m.ModelDir(root)
	if _, err := os.Stat(filepath.Join(dir, "tokens.txt")); err != nil {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, m.File))
	return err == nil
}

func ValidCustomModelDir(dir string) bool {
	if dir == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "tokens.txt")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "model.onnx")); err == nil {
		return true
	}
	_, err := os.Stat(filepath.Join(dir, "model.int8.onnx"))
	return err == nil
}

func HasBothPrecisions(dir string) bool {
	if dir == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "model.onnx")); err != nil {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, "model.int8.onnx"))
	return err == nil
}

func DownloadModel(ctx context.Context, spec ModelSpec, root, urlOverride string, onProgress func(pct float64)) error {
	url := spec.URL
	if urlOverride != "" {
		url = urlOverride
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}

	archive := filepath.Join(root, "."+spec.ID+".download")
	defer os.Remove(archive)
	if err := downloadFile(ctx, url, archive, "", onProgress); err != nil {
		return fmt.Errorf("download model %s: %w", spec.ID, err)
	}

	staging := filepath.Join(root, "."+spec.ID+"-staging")
	os.RemoveAll(staging)
	defer os.RemoveAll(staging)
	if err := untar(archive, staging); err != nil {
		return fmt.Errorf("extract model %s: %w", spec.ID, err)
	}
	os.Remove(archive)

	src := filepath.Join(staging, spec.Dir)
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("model archive did not contain %s", spec.Dir)
	}
	dest := spec.ModelDir(root)
	if _, err := os.Stat(dest); err == nil {
		return nil
	}
	return os.Rename(src, dest)
}
