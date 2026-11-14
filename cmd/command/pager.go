package command

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
)

// StartPager returns a writer that sends w's output through the command in
// HAZEL_PAGER, when HAZEL_PAGER is set, w is a file and either pager is true
// (--pager) or pager is nil (neither --pager nor --no-pager) and w is a
// terminal. Otherwise it returns w. The command runs with sh -c, or cmd /c
// on Windows.
//
// The returned function closes the pager's input and waits for it to exit.
// It can be called more than once.
func StartPager(w io.Writer, pager *bool) (io.Writer, func(), error) {
	noop := func() {}
	cmdline := os.Getenv("HAZEL_PAGER")
	f, ok := w.(*os.File)
	if cmdline == "" || !ok || (pager != nil && !*pager) || (pager == nil && !isTerminal(f)) {
		return w, noop, nil
	}
	cmd := exec.Command("sh", "-c", cmdline)
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", cmdline)
	}
	cmd.Stdout = f
	cmd.Stderr = os.Stderr
	// An io.Pipe would block hazel when the pager quits before reading all
	// of the output; the pipe of StdinPipe fails the writes instead.
	stdin, err := cmd.StdinPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		return w, noop, fmt.Errorf("failed to start the pager %q: %w", cmdline, err)
	}
	var once sync.Once
	return stdin, func() {
		once.Do(func() {
			stdin.Close() //nolint:errcheck
			cmd.Wait()    //nolint:errcheck
		})
	}, nil
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
