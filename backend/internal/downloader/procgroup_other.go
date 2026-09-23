//go:build !unix

package downloader

import (
	"os/exec"
	"time"
)

func killGroupOnCancel(cmd *exec.Cmd) {
	cmd.WaitDelay = 5 * time.Second
}
