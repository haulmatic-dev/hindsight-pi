package mirroraudit

import (
	"os/exec"
)

// execLookPath resolves a binary name. Package-level seam for tests.
var execLookPath = exec.LookPath

// execCommand runs the br binary in the store dir and returns its stdout.
var execCommand = func(path string, args []string, dir string) (string, error) {
	cmd := exec.Command(path, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}
