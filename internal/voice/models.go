package voice

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Recognizer kinds sent to the helper in set_model.
const (
	ModelKindSenseVoice     = "sensevoice"
	ModelKindSenseVoiceInt8 = "sensevoice-int8"
	ModelKindParaformer     = "paraformer"
)

// ModelSpec describes one downloadable offline ASR model.
type ModelSpec struct {
	ID   string // stable id persisted in settings
	Name string // display name
	Kind string // recognizer kind sent to the helper
	Dir  string // model directory under the models root
	File string // model file that must exist after install
	URL  string // tar.bz2 archive URL
	Size string // download size for display
}

const senseVoice20240717 = "sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17"

var modelCatalog = []ModelSpec{
	{
		// The archive carries both fp32 (model.onnx) and int8
		// (model.int8.onnx) weights; the precision is a settings toggle,
		// not a separate download.
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

// ModelCatalog is the list of selectable offline models, default first.
func ModelCatalog() []ModelSpec { return modelCatalog }

// ModelByID resolves a persisted model id; unknown ids fall back to the
// default (first catalog entry).
func ModelByID(id string) ModelSpec {
	for _, m := range modelCatalog {
		if m.ID == id {
			return m
		}
	}
	return modelCatalog[0]
}

// LegacyModelID maps the pre-merge catalog ids (sensevoice-fp32,
// sensevoice-int8) to the merged entry, reporting the precision they implied.
func LegacyModelID(id string) (newID string, int8 bool, legacy bool) {
	switch id {
	case "sensevoice-fp32":
		return modelCatalog[0].ID, false, true
	case "sensevoice-int8":
		return modelCatalog[0].ID, true, true
	}
	return "", false, false
}

// ModelsRoot is where downloaded models live.
func ModelsRoot() string {
	return filepath.Join(DefaultCacheDir(), "voice-models")
}

// ModelDir is the installed directory of the model under root.
func (m ModelSpec) ModelDir(root string) string {
	return filepath.Join(root, m.Dir)
}

// Installed reports whether the model files are present under root.
func (m ModelSpec) Installed(root string) bool {
	dir := m.ModelDir(root)
	if _, err := os.Stat(filepath.Join(dir, "tokens.txt")); err != nil {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, m.File))
	return err == nil
}

// ValidCustomModelDir reports whether dir holds a usable SenseVoice model:
// tokens.txt plus model.onnx or model.int8.onnx.
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

// HasBothPrecisions reports whether dir holds both fp32 and int8 weights,
// i.e. the precision toggle applies to a custom model directory.
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

// DownloadModel downloads and extracts the model archive into root via a
// staging dir and an atomic rename. urlOverride replaces the catalog URL
// when non-empty.
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
		// another process installed it meanwhile
		return nil
	}
	return os.Rename(src, dest)
}
