package ai

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

const maxSleepSeconds = 600

type SleepInput struct {
	Seconds int `json:"seconds" jsonschema_description:"Seconds to wait (1-600)"`
}

type SleepOutput struct {
	SleptSeconds float64 `json:"slept_seconds"`
}

func buildSleepTool() (tool.BaseTool, error) {
	t, err := utils.InferTool("sleep", "Wait for a duration while a long-running command or build finishes, instead of polling read_tab in a tight loop. send_keys' wait_ms already covers short waits after a keypress (a few seconds); sleep covers long ones, up to 600s. Read the tab after sleeping to check the result", sleep)
	if err != nil {
		return nil, fmt.Errorf("build sleep: %w", err)
	}
	return t, nil
}

func sleep(ctx context.Context, in *SleepInput) (*SleepOutput, error) {
	sec := in.Seconds
	if sec < 1 {
		sec = 1
	}
	if sec > maxSleepSeconds {
		sec = maxSleepSeconds
	}
	start := time.Now()
	timer := time.NewTimer(time.Duration(sec) * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
	return &SleepOutput{SleptSeconds: time.Since(start).Seconds()}, nil
}
