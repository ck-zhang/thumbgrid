package term

import (
	"encoding/base64"
	"fmt"
	"os"
)

type kittyRenderer struct{}

func (k *kittyRenderer) Name() string { return "kitty" }

func (k *kittyRenderer) ClearAll() error {
	_, _ = fmt.Fprint(os.Stdout, "\x1b_Ga=d,q=2;\x1b\\")
	return nil
}

func (k *kittyRenderer) Draw(path string, cellX, cellY, cellW, cellH int) error {
	if cellW <= 0 || cellH <= 0 || path == "" {
		return nil
	}
	pb64 := base64.StdEncoding.EncodeToString([]byte(path))
	cmd := fmt.Sprintf("\x1b[%d;%dH\x1b_Ga=T,t=f,f=100,c=%d,C=1,q=2;%s\x1b\\",
		cellY, cellX, cellW, pb64)
	Lock()
	defer Unlock()
	_, err := fmt.Fprint(os.Stdout, cmd)
	return err
}

func (k *kittyRenderer) Close() error { return nil }
