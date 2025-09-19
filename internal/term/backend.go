package term

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
	xt "golang.org/x/term"
)

type Renderer interface {
	Name() string
	ClearAll() error
	Draw(path string, cellX, cellY, cellW, cellH int) error
	Close() error
}

var writeMu sync.Mutex

func Lock()   { writeMu.Lock() }
func Unlock() { writeMu.Unlock() }

func Detect(pref string) (string, error) {
	p := strings.ToLower(strings.TrimSpace(pref))
	switch p {
	case "kitty":
		if kittyProtocolAvailable(75 * time.Millisecond) {
			return "kitty", nil
		}
		return "", errors.New("kitty graphics protocol not available")
	case "auto", "":
		if kittyProtocolAvailable(75 * time.Millisecond) {
			return "kitty", nil
		}
		return "none", nil
	default:
		return "", errors.New("unknown backend: " + pref)
	}
}

func kittyProtocolAvailable(timeout time.Duration) bool {
	if timeout <= 0 {
		timeout = 50 * time.Millisecond
	}
	stdin := os.Stdin
	stdout := os.Stdout
	if stdin == nil || stdout == nil {
		return false
	}
	fdIn := int(stdin.Fd())
	fdOut := int(stdout.Fd())
	if fdIn < 0 || fdOut < 0 {
		return false
	}
	if !xt.IsTerminal(fdIn) || !xt.IsTerminal(fdOut) {
		return false
	}
	query := "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\"
	if _, err := fmt.Fprint(stdout, query); err != nil {
		return false
	}
	_ = stdout.Sync()
	oldFlags, err := unix.FcntlInt(uintptr(fdIn), unix.F_GETFL, 0)
	if err != nil {
		return false
	}
	defer func() {
		_, _ = unix.FcntlInt(uintptr(fdIn), unix.F_SETFL, oldFlags)
	}()
	if err := unix.SetNonblock(fdIn, true); err != nil {
		return false
	}
	deadline := time.Now().Add(timeout)
	buf := make([]byte, 512)
	var acc bytes.Buffer
	for time.Now().Before(deadline) {
		remaining := int(time.Until(deadline) / time.Millisecond)
		if remaining <= 0 {
			remaining = 1
		}
		fds := []unix.PollFd{{Fd: int32(fdIn), Events: unix.POLLIN}}
		_, err := unix.Poll(fds, remaining)
		if err != nil {
			return false
		}
		if fds[0].Revents&unix.POLLIN == 0 {
			continue
		}
		n, err := unix.Read(fdIn, buf)
		if n > 0 {
			acc.Write(buf[:n])
			if bytes.Contains(acc.Bytes(), []byte("\x1b_G")) {
				return true
			}
		}
		if err != nil && err != unix.EAGAIN {
			return false
		}
	}
	return false
}

func New(backend string) (Renderer, error) {
	b := strings.ToLower(backend)
	switch b {
	case "kitty":
		return &kittyRenderer{}, nil
	case "none":
		return &noopRenderer{}, nil
	default:
		return nil, fmt.Errorf("unknown backend: %s", backend)
	}
}

type noopRenderer struct{}

func (n *noopRenderer) Name() string                          { return "none" }
func (n *noopRenderer) ClearAll() error                       { return nil }
func (n *noopRenderer) Draw(string, int, int, int, int) error { return nil }
func (n *noopRenderer) Close() error                          { return nil }
