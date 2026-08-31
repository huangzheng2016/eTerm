package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var version = "0.1.0"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	modelRoot := flag.String("model-dir", "", "model cache directory (default: user cache dir)")
	decodeWav := flag.String("decode", "", "decode a 16kHz wav file and exit (smoke test)")
	flag.Parse()

	if *showVersion {
		fmt.Printf("voicehelper %s (protocol %d)\n", version, protocolVersion)
		return
	}

	root := *modelRoot
	if root == "" {
		root = defaultModelRoot()
	}

	ev := newEventWriter(os.Stdout)

	if *decodeWav != "" {
		decodeFile(*decodeWav, root, ev)
		return
	}

	ev.emit(Event{Type: "hello", Version: version, Protocol: protocolVersion})

	eng := newASREngine(ev, root)
	defer eng.close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		cancel()
	}()

	cmds := make(chan Command, 16)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Bytes()
			if len(line) == 0 {
				continue
			}
			var cmd Command
			if err := json.Unmarshal(line, &cmd); err != nil {
				ev.errorf("bad command: %v", err)
				continue
			}
			cmds <- cmd
		}
		cancel()
	}()

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case cmd := <-cmds:
			switch cmd.Cmd {
			case "start":
				eng.start(ctx)
			case "stop":
				eng.stop()
			case "set_model":
				eng.setModel(cmd.Path)
			case "set_vad_params":
				eng.setVADParams(cmd)
			default:
				ev.errorf("unknown command: %s", cmd.Cmd)
			}
		case chunk := <-eng.cap.chunks:
			eng.onChunk(chunk, time.Now())
		case <-ticker.C:
			// timer checks (trailing silence, no-speech timeout) run even
			// when no audio arrives
			eng.onChunk(nil, time.Now())
		}
	}
}
