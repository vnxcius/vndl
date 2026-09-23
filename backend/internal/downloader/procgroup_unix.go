//go:build unix

package downloader

import (
	"os/exec"
	"syscall"
	"time"
)

// killGroupOnCancel kills yt-dlp's whole process group on cancel, so an
// ffmpeg child it spawned doesn't outlive it.
func killGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
}
