package command

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
)

// StartPager returns a writer that sends w's output through the command in
// HAZEL_PAGER, when HAZEL_PAGER is set, w is a file and either pager is true
// (--pager) or pager is nil (neither --pager nor --no-pager) and w is a
// terminal. Otherwise it returns w. The command runs with sh -c, or cmd /c
// on Windows. A shell that is not found is an error.
//
// The pager starts on the first write, so a command that writes nothing, or
// fails before it writes, does not open it. The returned function closes the
// pager's input and waits for it to exit. It can be called more than once.
func StartPager(w io.Writer, pager *bool) (io.Writer, func(), error) {
	cmdline := os.Getenv("HAZEL_PAGER")
	f, ok := w.(*os.File)
	if cmdline == "" || !ok || (pager != nil && !*pager) || (pager == nil && !isTerminal(f)) {
		return w, func() {}, nil
	}
	cmd := exec.Command("sh", "-c", cmdline)
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", cmdline)
	}
	if cmd.Err != nil {
		return w, func() {}, fmt.Errorf("failed to start the pager %q: %w", cmdline, cmd.Err)
	}
	cmd.Stdout = f
	cmd.Stderr = os.Stderr
	p := &pagerWriter{cmd: cmd, out: f}
	return p, p.close, nil
}

// pagerWriter starts the pager on its first write. A pager that does not
// start leaves the output to go to the file directly.
type pagerWriter struct {
	cmd     *exec.Cmd
	out     io.Writer
	stdin   io.WriteCloser // nil until the pager runs
	started bool
}

func (p *pagerWriter) Write(b []byte) (int, error) {
	if !p.started {
		p.started = true
		// An io.Pipe would block hazel when the pager quits before reading
		// all of the output; the pipe of StdinPipe fails the writes instead.
		stdin, err := p.cmd.StdinPipe()
		if err == nil {
			err = p.cmd.Start()
		}
		if err == nil {
			p.out, p.stdin = stdin, stdin
		}
	}
	return p.out.Write(b)
}

func (p *pagerWriter) close() {
	if p.stdin != nil {
		p.stdin.Close() //nolint:errcheck
		p.cmd.Wait()    //nolint:errcheck
		p.stdin = nil
	}
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
