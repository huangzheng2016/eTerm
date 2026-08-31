# voicehelper

Minimal sherpa-onnx voice helper for eTerm. Separate Go module (CGO) so the
main eTerm module stays pure Go.

Pipeline: malgo mic capture (16 kHz mono S16, 20 ms chunks) -> Silero VAD
(sherpa-onnx) -> SenseVoice offline ASR (sherpa-onnx) -> NDJSON events on
stdout. Commands arrive as NDJSON on stdin.

## Protocol

First line on stdout is the handshake:

```json
{"type":"hello","version":"0.1.0","protocol":1}
```

Commands (stdin, one JSON object per line):

- `{"cmd":"start"}` - start capture; downloads models on first use
- `{"cmd":"stop"}` - stop capture; pending speech is decoded and emitted as final
- `{"cmd":"set_model","path":"/path/to/model-dir"}` - switch ASR model dir
  (must contain tokens.txt and model.onnx or model.int8.onnx); empty path
  resets to the auto-downloaded default
- `{"cmd":"set_vad_params","threshold":0.5,"min_silence":0.3,"min_speech":0.2,"trailing_silence":1.0,"max_segment":30,"no_speech_timeout":5}` -
  all fields optional, applied live

Events (stdout):

- `{"type":"state","state":"idle|listening|speech|silence"}`
- `{"type":"partial","text":"..."}` - accumulated text so far
- `{"type":"final","text":"..."}` - sentence finalized (after ~1s trailing
  silence, max segment length, or stop)
- `{"type":"error","msg":"..."}`
- `{"type":"download_progress","pct":42.0}`

VAD behavior: a trigger finalizes after trailing_silence (default 1.0s) of
silence after speech, so multiple sentences fit in one trigger. Speech is
force-finalized at max_segment (default 30s). If no speech is detected within
no_speech_timeout (default 5s) of a trigger, the session cancels to idle.

## Models

On first `start` the helper downloads into the model dir (`-model-dir`, default
`os.UserCacheDir()/eterm/voice-models`):

- `silero_vad.onnx` from k2-fsa/sherpa-onnx releases (asr-models)
- `sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17.tar.bz2` from k2-fsa
  releases, extracted; fp32 `model.onnx` is preferred over `model.int8.onnx`

## Build / CI (release artifact recipe)

The sherpa-onnx-go Go modules ship prebuilt shared libraries, so a plain
build works; the resulting binary has an rpath into the Go module cache and
therefore only runs on the build machine:

```sh
cd cmd/voicehelper
go build -o voicehelper .
```

To produce the distributable artifact that eTerm downloads at runtime (per
the sherpa-onnx-go README's native-library step), stage the dylibs next to
the binary and rewrite the rpath. CI recipe for macOS arm64:

```sh
cd cmd/voicehelper
CGO_ENABLED=1 go build -o dist/voicehelper .
SHERPA_LIB=$(go env GOMODCACHE)/github.com/k2-fsa/sherpa-onnx-go-macos@v1.13.3/lib/aarch64-apple-darwin
cp "$SHERPA_LIB"/libsherpa-onnx-c-api.dylib "$SHERPA_LIB"/libonnxruntime.1.24.4.dylib dist/
cd dist
ln -sf libonnxruntime.1.24.4.dylib libonnxruntime.dylib
install_name_tool -add_rpath @executable_path voicehelper
codesign --sign - --force voicehelper *.dylib   # ad-hoc sign for Gatekeeper
tar -czf voicehelper-darwin-arm64.tar.gz voicehelper *.dylib
shasum -a 256 voicehelper-darwin-arm64.tar.gz   # publish checksum with the release
```

Linux x86_64 equivalent: use `sherpa-onnx-go-linux@v1.13.3/lib/x86_64-unknown-linux-gnu`
and patchelf instead of install_name_tool. Upload the tarball plus its
sha256 as the release asset eTerm's internal/voice package downloads.
The artifact contract: `voicehelper-<os>-<arch>.tar.gz` with the binary
(named `voicehelper`) and its dylibs flat at the top level; eTerm verifies
the sha256 and extracts it into a cache dir so the dylibs sit next to the
binary for @executable_path.
