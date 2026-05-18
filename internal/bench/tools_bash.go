package bench

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// RunBash executes cmd via /bin/sh -c in workdir with a restricted PATH
// (only /usr/bin and /bin). Returns combined stdout+stderr or error.
// Exit-code failures return the combined output without an error so the
// agent can read stderr and react; real exec failures and deadline
// timeouts bubble as errors.
func RunBash(workdir, cmd string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	c := exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
	c.Dir = workdir
	c.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + workdir}
	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf
	err := c.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return buf.String(), fmt.Errorf("timeout after %v", timeout)
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return buf.String(), nil
		}
		return buf.String(), err
	}
	return buf.String(), nil
}
