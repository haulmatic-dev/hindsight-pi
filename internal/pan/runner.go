package pan

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RunPi is the production Runner: pi headless (same invocation shape as the
// gap adjudicator), prompt on stdin, Pan's own provider identity, API key
// injected from the named env var.
func RunPi(ctx context.Context, cfg Config, prompt string) (string, error) {
	// --no-extensions: Pan's prompt is fully self-contained (JSON in, JSON
	// out). The full interactive extension load measured 48,315 vs 12,059
	// tokens/call on the worker path (2026-09) — a per-judgment tax Pan
	// never needed to pay.
	var args []string
	args = append(args, "--no-extensions")
	if cfg.Provider != "" {
		args = append(args, "--provider", cfg.Provider)
	}
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	if cfg.Thinking != "" {
		args = append(args, "--thinking-level", cfg.Thinking)
	}

	piPath := cfg.PiPath
	if env := os.Getenv("HYDRA_PI_PATH"); env != "" {
		piPath = env
	}
	c := exec.CommandContext(ctx, piPath, args...)
	c.Stdin = strings.NewReader(prompt)
	c.Env = os.Environ()
	if cfg.APIKeyName != "" {
		if key := os.Getenv(cfg.APIKeyName); key != "" {
			c.Env = append(c.Env, fmt.Sprintf("%s=%s", cfg.APIKeyName, key))
		}
	}
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		tail := strings.TrimSpace(stderr.String())
		if len(tail) > 200 {
			tail = tail[len(tail)-200:]
		}
		return "", fmt.Errorf("pi exited: %w (stderr: %s)", err, tail)
	}
	return stdout.String(), nil
}
