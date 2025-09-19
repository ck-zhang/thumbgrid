//go:build !windows

package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ck-zhang/thumbgrid/internal/term"
	"github.com/ck-zhang/thumbgrid/internal/thumb"
	runewidth "github.com/mattn/go-runewidth"
	xt "golang.org/x/term"
)

var (
	version   = "0.0.0-dev"
	buildDate = ""
)

type Config struct {
	Path     string
	CacheDir string
	Filter   string
	SortBy   string
	Order    string
}

type Candidate struct {
	Path  string
	Name  string
	Size  int64
	MTime time.Time
	Kind  string
}

const (
	filterBoth   = "both"
	filterImages = "images"
	filterVideos = "videos"
)

func main() {
	cfg, err := parseFlags()
	if err != nil {
		fatalUsage(64, err.Error())
	}
	if cfg.Path == "" {
		cfg.Path = "."
	}
	cands, err := scanPath(cfg.Path, cfg)
	if err != nil {
		fatalUsage(65, "scan error: %v", err)
	}

	cands = filterCandidates(cands, cfg.Filter)
	if len(cands) == 0 {
		fatalUsage(66, "no candidates for filter %q in %s", cfg.Filter, toAbs(cfg.Path))
	}

	if err := sortCandidates(cands, cfg.SortBy, cfg.Order); err != nil {
		fatalUsage(65, "sort: %v", err)
	}

	sel := []string{}
	if isTerminal(os.Stdin.Fd()) && isTerminal(os.Stdout.Fd()) {
		out, code, err := runGridTUI(cands, cfg)
		if err != nil {
			fatalUsage(code, err.Error())
		}
		sel = out
	} else {

		sel = make([]string, 0, len(cands))
		for _, c := range cands {
			sel = append(sel, toAbs(c.Path))
		}
	}

	for _, p := range sel {
		fmt.Fprintln(os.Stdout, p)
	}

	os.Exit(0)
}

func parseFlags() (Config, error) {
	help := flag.Bool("help", false, "Show help")
	showVersion := flag.Bool("version", false, "Print version and exit")
	filter := flag.String("filter", "both", "Filter: image|video|both")
	sortBy := flag.String("sort", "mtime", "Sort: name|mtime|size")
	order := flag.String("order", "desc", "Order: asc|desc")
	flag.Parse()

	if *help {
		fmt.Fprintln(os.Stdout, `thumbgrid [PATH]

Minimal grid selector for images and videos.

Options:
  -filter image|video|both    Filter candidate types
  -sort name|mtime|size       Sort order field
  -order asc|desc             Sort direction
  -version                    Print version and exit
  -help                       Show this help text

Keys:
  arrows / hjkl               Move selection
  PgUp / PgDn                 Scroll by a page
  Ctrl-B / Ctrl-F             Scroll by a page
  g g                         Jump to top
  G                           Jump to bottom
  + / -                       Resize tiles
  p                           Toggle previews
  Enter                       Accept selection(s)
  q / Esc                     Cancel

Environment:
  THUMBGRID_CACHE_DIR         Override cache directory`)
		os.Exit(0)
	}
	if *showVersion {
		fmt.Fprintf(os.Stdout, "thumbgrid %s", version)
		if buildDate != "" {
			fmt.Fprintf(os.Stdout, " (%s)", buildDate)
		}
		fmt.Fprintln(os.Stdout)
		os.Exit(0)
	}

	args := flag.Args()
	var path string
	if len(args) > 0 {
		path = args[0]
	}
	normFilter, err := normalizeFilter(*filter)
	if err != nil {
		return Config{}, err
	}

	return Config{Path: path, CacheDir: defaultCacheDir(), Filter: normFilter, SortBy: *sortBy, Order: *order}, nil
}

func normalizeFilter(filter string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(filter)) {
	case "", filterBoth, "all":
		return filterBoth, nil
	case "image", filterImages:
		return filterImages, nil
	case "video", filterVideos:
		return filterVideos, nil
	default:
		return "", fmt.Errorf("invalid filter %q (expected image(s), video(s), or both)", filter)
	}
}

func fatalUsage(code int, format string, a ...any) {
	fmt.Fprintf(os.Stderr, "thumbgrid: "+format+"\n", a...)
	os.Exit(code)
}

func defaultCacheDir() string {
	if v := os.Getenv("THUMBGRID_CACHE_DIR"); v != "" {
		return v
	}
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, "thumbgrid")
	}
	if x := os.Getenv("XDG_CACHE_HOME"); x != "" {
		return filepath.Join(x, "thumbgrid")
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		return ".thumbgrid-cache"
	}
	return filepath.Join(home, ".cache", "thumbgrid")
}

func scanPath(root string, cfg Config) ([]Candidate, error) {
	var cands []Candidate
	cacheAbs := toAbs(cfg.CacheDir)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {

			if toAbs(path) == cacheAbs {
				return filepath.SkipDir
			}
			return nil
		}
		kind := classify(path)
		if !passes(kind, cfg.Filter) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		cands = append(cands, Candidate{
			Path:  path,
			Name:  d.Name(),
			Size:  info.Size(),
			MTime: info.ModTime(),
			Kind:  kind,
		})
		return nil
	})
	return cands, err
}

func classify(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".tif", ".tiff", ".avif", ".heic":
		return "image"
	case ".mp4", ".mov", ".mkv", ".webm", ".avi", ".m4v":
		return "video"
	default:
		return "other"
	}
}

func filterCandidates(in []Candidate, mode string) []Candidate {
	out := in[:0]
	for _, c := range in {
		if passes(c.Kind, mode) {
			out = append(out, c)
		}
	}
	return out
}

func passes(kind, filter string) bool {
	switch filter {
	case filterImages:
		return kind == "image"
	case filterVideos:
		return kind == "video"
	case filterBoth, "":
		return kind == "image" || kind == "video"
	default:
		return false
	}
}

func sortCandidates(cands []Candidate, by, order string) error {
	desc := strings.EqualFold(order, "desc")
	switch by {
	case "name":
		sort.Slice(cands, func(i, j int) bool {
			a, b := strings.ToLower(cands[i].Name), strings.ToLower(cands[j].Name)
			if desc {
				return a > b
			}
			return a < b
		})
	case "mtime":
		sort.Slice(cands, func(i, j int) bool {
			if desc {
				return cands[i].MTime.After(cands[j].MTime)
			}
			return cands[i].MTime.Before(cands[j].MTime)
		})
	case "size":
		sort.Slice(cands, func(i, j int) bool {
			if desc {
				return cands[i].Size > cands[j].Size
			}
			return cands[i].Size < cands[j].Size
		})
	default:
		return fmt.Errorf("invalid sort: %s", by)
	}
	return nil
}

func toAbs(p string) string {
	if p == "" {
		return p
	}
	if filepath.IsAbs(p) {
		return p
	}
	ap, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return ap
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func isTerminal(fd uintptr) bool { return xt.IsTerminal(int(fd)) }

func humanSize(n int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case n >= GB:
		return fmt.Sprintf("%.1fG", float64(n)/float64(GB))
	case n >= MB:
		return fmt.Sprintf("%.1fM", float64(n)/float64(MB))
	case n >= KB:
		return fmt.Sprintf("%.1fK", float64(n)/float64(KB))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

func sanitizePrintable(s string) string {
	rs := []rune(s)
	b := make([]rune, 0, len(rs))
	for _, r := range rs {
		if r == '\x1b' || r == '\n' || r == '\r' || r == '\t' || (r < 0x20) || (r == 0x7f) {
			b = append(b, ' ')
		} else {
			b = append(b, r)
		}
	}
	return string(b)
}

func dispWidth(s string) int { return runewidth.StringWidth(s) }

func truncateMiddleDisp(s string, width int) string {
	s = sanitizePrintable(s)
	if width <= 0 {
		return ""
	}
	if dispWidth(s) <= width {
		return s
	}
	if width <= 3 {
		return runewidth.Truncate(s, width, "")
	}
	avail := width - 3
	left := avail / 2
	right := avail - left
	rs := []rune(s)

	lPart := make([]rune, 0, len(rs))
	w := 0
	for _, r := range rs {
		rw := runewidth.RuneWidth(r)
		if w+rw > left {
			break
		}
		lPart = append(lPart, r)
		w += rw
	}

	rPart := make([]rune, 0, len(rs))
	w = 0
	for i := len(rs) - 1; i >= 0; i-- {
		r := rs[i]
		rw := runewidth.RuneWidth(r)
		if w+rw > right {
			break
		}
		rPart = append(rPart, r)
		w += rw
	}

	for i, j := 0, len(rPart)-1; i < j; i, j = i+1, j-1 {
		rPart[i], rPart[j] = rPart[j], rPart[i]
	}
	out := string(lPart) + "..." + string(rPart)

	if dispWidth(out) > width {
		out = runewidth.Truncate(out, width, "")
	}
	return out
}

func padRightToWidth(s string, w int) string {
	sw := dispWidth(s)
	if sw >= w {
		if sw == w {
			return s
		}
		return runewidth.Truncate(s, w, "")
	}
	return s + strings.Repeat(" ", w-sw)
}

func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}

func runGridTUI(cands []Candidate, cfg Config) ([]string, int, error) {
	fdIn := int(os.Stdin.Fd())
	old, err := xt.MakeRaw(fdIn)
	if err != nil {
		return nil, 65, fmt.Errorf("raw mode: %w", err)
	}
	defer xt.Restore(fdIn, old)

	fmt.Fprint(os.Stdout, "\x1b[?1000h\x1b[?1002h\x1b[?1006h")
	defer fmt.Fprint(os.Stdout, "\x1b[?1006l\x1b[?1002l\x1b[?1000l")
	bname, _ := term.Detect("auto")
	renderer, _ := term.New(bname)
	useGraphics := renderer != nil && renderer.Name() != "none"
	var sched *term.Scheduler
	if useGraphics {
		sched = term.NewScheduler(renderer, 128)

		defer func() { _ = renderer.ClearAll() }()
		defer func() { sched.Close() }()
	}

	cur := 0
	topRow := 0
	awaitGG := false
	showImages := useGraphics

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)

	w, h, _ := xt.GetSize(int(os.Stdout.Fd()))
	if h <= 0 {
		h = 24
	}
	if w <= 0 {
		w = 80
	}

	headerH := 1
	footerH := 1
	contentY := headerH + 1
	contentH := h - headerH - footerH
	if contentH < 0 {
		contentH = 0
	}

	zoom := 0
	baseTileW, baseTileH := 18, 6
	gutter := 2
	ppcX, ppcY := 10, 20
	clampTile := func(wd, ht int) (int, int) {
		if wd < 8 {
			wd = 8
		}
		if ht < 3 {
			ht = 3
		}
		return wd, ht
	}

	computeLayout := func() (gridX, gridY, gridW, gridH, tileW, tileH, cols, rows int) {
		gridX, gridY = 1, contentY
		gridW, gridH = w, contentH

		tileW = baseTileW + zoom*4
		tileH = baseTileH + zoom*2
		tileW, tileH = clampTile(tileW, tileH)

		stepW := tileW + gutter
		if gridW < tileW {
			cols = 1
		} else {
			cols = (gridW + gutter) / stepW
		}
		if cols < 1 {
			cols = 1
		}
		stepH := tileH + gutter

		if gridH < tileH {
			rows = 0
		} else {
			rows = 1 + (gridH-tileH)/stepH
		}
		return
	}

	repaintCh := make(chan struct{}, 1)

	type thumbKey struct {
		path     string
		wpx, hpx int
	}
	thumbReady := make(map[thumbKey]string)
	thumbInflight := make(map[thumbKey]struct{})
	var thumbMu sync.Mutex
	thumbQ := make(chan thumbKey, 256)
	quitThumb := make(chan struct{})
	workers := 4
	for i := 0; i < workers; i++ {
		go func() {
			for {
				select {
				case k := <-thumbQ:
					tp, err := thumb.GenerateRect(k.path, k.wpx, k.hpx, cfg.CacheDir)
					thumbMu.Lock()
					if err == nil {
						thumbReady[k] = tp
					}
					delete(thumbInflight, k)
					thumbMu.Unlock()
					select {
					case repaintCh <- struct{}{}:
					default:
					}
				case <-quitThumb:
					return
				}
			}
		}()
	}
	defer close(quitThumb)

	ensureThumb := func(path string, wpx, hpx int) (string, bool) {
		k := thumbKey{path: path, wpx: wpx, hpx: hpx}
		thumbMu.Lock()
		if tp, ok := thumbReady[k]; ok {
			thumbMu.Unlock()
			return tp, true
		}
		if _, inflight := thumbInflight[k]; !inflight {
			thumbInflight[k] = struct{}{}
			select {
			case thumbQ <- k:
			default:
			}
		}
		thumbMu.Unlock()
		return "", false
	}

	drawTile := func(idx, px, py, tileW, tileH int, renderImages bool) {
		innerW := tileW - 2
		if innerW < 2 {
			innerW = 2
		}
		corner := "+"
		hChar := "-"
		if idx >= 0 && idx < len(cands) && idx == cur {
			hChar = "="
			corner = "*"
		}
		top := corner + strings.Repeat(hChar, max(0, tileW-2)) + corner
		bot := top
		fmt.Fprintf(os.Stdout, "\x1b[%d;%dH%s", py, px, top)
		fmt.Fprintf(os.Stdout, "\x1b[%d;%dH%s", py+tileH-1, px, bot)

		for rr := 1; rr < tileH-1; rr++ {
			fmt.Fprintf(os.Stdout, "\x1b[%d;%dH|", py+rr, px)
			fmt.Fprintf(os.Stdout, "\x1b[%d;%dH|", py+rr, px+tileW-1)
		}

		if idx < 0 || idx >= len(cands) {
			for r := 1; r < tileH-1; r++ {
				fmt.Fprintf(os.Stdout, "\x1b[%d;%dH|%s|", py+r, px, strings.Repeat(" ", innerW))
			}
			return
		}

		c := cands[idx]
		imgH := max(1, tileH-3)
		isImg := c.Kind == "image" || c.Kind == "video"
		if renderImages || !useGraphics || !isImg {
			for r := 1; r < tileH-1; r++ {
				fmt.Fprintf(os.Stdout, "\x1b[%d;%dH|%s|", py+r, px, strings.Repeat(" ", innerW))
			}
		}
		if renderImages && isImg {
			wpx := max(8, innerW*ppcX)
			hpx := max(8, imgH*ppcY)
			if tp, ok := ensureThumb(c.Path, wpx, hpx); ok && sched != nil {
				sched.Enqueue(tp, px+1, py+1, innerW, imgH)
			}
		}
		if !(renderImages && isImg) {
			icon := otherIcon(c.Path)
			if dispWidth(icon) > innerW {
				icon = runewidth.Truncate(icon, innerW, "")
			}
			ix := px + 1 + max(0, (innerW-dispWidth(icon))/2)
			iy := py + 1 + max(0, (imgH-1)/2)
			fmt.Fprintf(os.Stdout, "\x1b[%d;%dH%s", iy, ix, icon)
		}
		name := truncateMiddleDisp(c.Name, innerW-3)
		line := fmt.Sprintf("%c %s", ternary(idx == cur, '>', ' '), name)
		line = padRightToWidth(line, innerW)
		if tileH >= 3 {
			fmt.Fprintf(os.Stdout, "\x1b[%d;%dH|%s|", py+tileH-2, px, line)
		}
	}
	draw := func() {
		term.Lock()
		fmt.Fprint(os.Stdout, "\x1b[2J\x1b[H")
		header := fmt.Sprintf("[%s] Arrows/hjkl move • Enter accept • q/Esc cancel", ternary(useGraphics, renderer.Name(), "none"))
		if dispWidth(header) > w {
			header = runewidth.Truncate(header, w, "")
		}
		fmt.Fprintf(os.Stdout, "\x1b[1;1H%s\x1b[K", header)
		gridX, gridY, _, _, tileW, tileH, cols, rows := computeLayout()

		prefetchRows := 1
		if showImages && rows > 0 && cols > 0 {
			for r := -prefetchRows; r < rows+prefetchRows; r++ {
				rr := topRow + r
				if rr < 0 {
					continue
				}
				for ccol := 0; ccol < cols; ccol++ {
					idx := rr*cols + ccol
					if idx < 0 || idx >= len(cands) {
						continue
					}
					c := cands[idx]
					if c.Kind != "image" && c.Kind != "video" {
						continue
					}
					innerW := tileW - 2
					if innerW < 2 {
						innerW = 2
					}
					imgH := max(1, tileH-3)
					wpx := max(8, innerW*ppcX)
					hpx := max(8, imgH*ppcY)
					_, _ = ensureThumb(c.Path, wpx, hpx)
				}
			}
		}
		renderImages := showImages
		if rows > 0 && cols > 0 {
			for r := 0; r < rows; r++ {
				for ccol := 0; ccol < cols; ccol++ {
					idx := (topRow+r)*cols + ccol
					px := gridX + ccol*(tileW+gutter)
					py := gridY + r*(tileH+gutter)
					drawTile(idx, px, py, tileW, tileH, renderImages)
				}
			}
		}
		var status string
		if len(cands) > 0 {
			c := cands[cur]
			idx := cur + 1
			_, _, _, _, tileW, tileH, cols, rows = computeLayout()
			status = fmt.Sprintf("%d/%d • Name: %s • Type: %s • Size: %s • Grid: %dx%d • Tile: %dx%d",
				idx, len(cands), truncateMiddleDisp(c.Name, max(10, w/3)), c.Kind, humanSize(c.Size), cols, rows, tileW, tileH)
		} else {
			status = "(no items)"
		}
		if h >= 2 {
			s := sanitizePrintable(status)
			if dispWidth(s) > w {
				s = runewidth.Truncate(s, w, "")
			}
			fmt.Fprintf(os.Stdout, "\x1b[%d;1H%s\x1b[K", h, s)
		}
		term.Unlock()
	}
	dataRows := func() int {
		_, _, _, _, _, _, cols, _ := computeLayout()
		return int((len(cands) + cols - 1) / cols)
	}
	curRow := func() int {
		_, _, _, _, _, _, cols, _ := computeLayout()
		return cur / cols
	}
	curCol := func() int {
		_, _, _, _, _, _, cols, _ := computeLayout()
		return cur % cols
	}

	moveTo := func(ncur int) {
		if ncur < 0 {
			ncur = 0
		}
		if ncur >= len(cands) {
			ncur = len(cands) - 1
		}
		cur = ncur
		r := curRow()
		if r < topRow {
			topRow = r
		}
		_, _, _, _, _, _, _, rows := computeLayout()
		if r >= topRow+rows {
			topRow = r - rows + 1
		}
		if topRow < 0 {
			topRow = 0
		}
		maxTop := max(0, dataRows()-rows)
		if topRow > maxTop {
			topRow = maxTop
		}
	}

	var stateMu sync.Mutex
	quitRender := make(chan struct{})
	var renderWG sync.WaitGroup
	requestRepaint := func() {
		select {
		case repaintCh <- struct{}{}:
		default:
		}
	}
	renderWG.Add(1)
	go func() {
		defer renderWG.Done()
		ticker := time.NewTicker(16 * time.Millisecond)
		defer ticker.Stop()
		dirty := true
		for {
			select {
			case <-quitRender:
				return
			case <-repaintCh:
				dirty = true
			case <-ticker.C:
				if !dirty {
					continue
				}
				if sched != nil {
					sched.NextFrame()
				}
				stateMu.Lock()
				draw()
				stateMu.Unlock()
				dirty = false
			}
		}
	}()
	defer func() { close(quitRender); renderWG.Wait() }()

	requestRepaint()
	br := bufio.NewReader(os.Stdin)
	for {
		select {
		case <-winch:
			w2, h2, _ := xt.GetSize(int(os.Stdout.Fd()))
			stateMu.Lock()
			if h2 > 0 {
				h = h2
			} else {
				h = 24
			}
			if w2 > 0 {
				w = w2
			} else {
				w = 80
			}
			contentH = h - headerH - footerH
			if contentH < 0 {
				contentH = 0
			}
			stateMu.Unlock()
			requestRepaint()
			awaitGG = false
			continue
		default:
		}
		b, err := br.ReadByte()
		if err != nil {
			return nil, 65, fmt.Errorf("read: %w", err)
		}
		switch b {
		case 'q':
			if renderer != nil {
				_ = renderer.ClearAll()
			}
			fmt.Fprint(os.Stdout, "\x1b[2J\x1b[H")
			return nil, 130, fmt.Errorf("canceled")
		case 0x03:
			if renderer != nil {
				_ = renderer.ClearAll()
			}
			fmt.Fprint(os.Stdout, "\x1b[2J\x1b[H")
			return nil, 130, fmt.Errorf("canceled")
		case 0x1b:
			if br.Buffered() == 0 {
				if renderer != nil {
					_ = renderer.ClearAll()
				}
				fmt.Fprint(os.Stdout, "\x1b[2J\x1b[H")
				return nil, 130, fmt.Errorf("canceled")
			}
			next, _ := br.ReadByte()
			if next == '[' {
				b3, _ := br.ReadByte()
				if b3 == '<' {
					buf := make([]byte, 0, 32)
					for {
						x, err := br.ReadByte()
						if err != nil {
							break
						}
						buf = append(buf, x)
						if x == 'M' || x == 'm' {
							break
						}
					}
					s := string(buf)
					parts := strings.Split(strings.TrimRight(s, "Mm"), ";")
					if len(parts) == 3 && parts[0] != "" {
						btn, _ := strconv.Atoi(parts[0])
						cx, _ := strconv.Atoi(parts[1])
						cy, _ := strconv.Atoi(parts[2])
						stateMu.Lock()
						gridX, gridY, _, _, tileW, tileH, cols, rows := computeLayout()
						stateMu.Unlock()
						_ = rows
						if cx >= gridX && cy >= gridY {
							offX := cx - gridX
							offY := cy - gridY
							stepW := tileW + gutter
							stepH := tileH + gutter
							ccol := offX / stepW
							rrow := offY / stepH

							if btn == 64 {
								stateMu.Lock()
								if topRow > 0 {
									topRow--
								}
								stateMu.Unlock()
								requestRepaint()
								awaitGG = false
								continue
							}
							if btn == 65 {
								stateMu.Lock()
								_, _, _, _, _, _, _, r := computeLayout()
								maxTop := max(0, dataRows()-r)
								if topRow < maxTop {
									topRow++
								}
								stateMu.Unlock()
								requestRepaint()
								awaitGG = false
								continue
							}
							if ccol >= 0 && ccol < cols && rrow >= 0 {
								px := gridX + ccol*stepW
								py := gridY + rrow*stepH
								if cx <= px+tileW-1 && cy <= py+tileH-1 {
									idx := (topRow+rrow)*cols + ccol
									if idx >= 0 && idx < len(cands) {
										if btn < 64 {
											stateMu.Lock()
											moveTo(idx)
											stateMu.Unlock()
											requestRepaint()
										}
									}
								}
							}
						}
					}
					awaitGG = false
					continue
				}
				switch b3 {
				case 'A':
					stateMu.Lock()
					_, _, _, _, _, _, cols, _ := computeLayout()
					if cur-cols >= 0 {
						moveTo(cur - cols)
					}
					stateMu.Unlock()
				case 'B':
					stateMu.Lock()
					_, _, _, _, _, _, cols, _ := computeLayout()
					if cur+cols < len(cands) {
						moveTo(cur + cols)
					}
					stateMu.Unlock()
				case 'C':
					stateMu.Lock()
					_, _, _, _, _, _, cols, _ := computeLayout()
					if (cur%cols) < cols-1 && cur+1 < len(cands) {
						moveTo(cur + 1)
					}
					stateMu.Unlock()
				case 'D':
					stateMu.Lock()
					_, _, _, _, _, _, cols, _ := computeLayout()
					if (cur % cols) > 0 {
						moveTo(cur - 1)
					}
					stateMu.Unlock()
				case '5':
					stateMu.Lock()
					_, _, _, _, _, _, _, rows := computeLayout()
					col := curCol()
					newRow := curRow() - rows
					if newRow < 0 {
						newRow = 0
					}
					_, _, _, _, _, _, cols, _ := computeLayout()
					idx := newRow*cols + col
					if idx >= len(cands) {
						idx = len(cands) - 1
					}
					moveTo(idx)
					stateMu.Unlock()
					_, _ = br.ReadByte()
				case '6':
					stateMu.Lock()
					_, _, _, _, _, _, _, rows := computeLayout()
					col := curCol()
					newRow := curRow() + rows
					maxRow := dataRows() - 1
					if newRow > maxRow {
						newRow = maxRow
					}
					_, _, _, _, _, _, cols, _ := computeLayout()
					idx := newRow*cols + col
					if idx >= len(cands) {
						idx = len(cands) - 1
					}
					moveTo(idx)
					stateMu.Unlock()
					_, _ = br.ReadByte()
				}
				requestRepaint()
				awaitGG = false
				continue
			}
			if renderer != nil {
				_ = renderer.ClearAll()
			}
			fmt.Fprint(os.Stdout, "\x1b[2J\x1b[H")
			return nil, 130, fmt.Errorf("canceled")
		case 0x0c:
			requestRepaint()
			awaitGG = false
		case 0x05:
			stateMu.Lock()
			_, _, _, _, _, _, _, rows := computeLayout()
			_ = rows
			maxTop := max(0, dataRows()-rows)
			if topRow < maxTop {
				topRow++
			}
			stateMu.Unlock()
			requestRepaint()
			awaitGG = false
		case 0x19:
			stateMu.Lock()
			if topRow > 0 {
				topRow--
			}
			stateMu.Unlock()
			requestRepaint()
			awaitGG = false
		case 0x04:
			stateMu.Lock()
			_, _, _, _, _, _, _, rows := computeLayout()
			delta := max(1, rows/2)
			maxTop := max(0, dataRows()-rows)
			topRow += delta
			if topRow > maxTop {
				topRow = maxTop
			}
			stateMu.Unlock()
			requestRepaint()
			awaitGG = false
		case 0x15:
			stateMu.Lock()
			_, _, _, _, _, _, _, rows := computeLayout()
			delta := max(1, rows/2)
			topRow -= delta
			if topRow < 0 {
				topRow = 0
			}
			stateMu.Unlock()
			requestRepaint()
			awaitGG = false
		case 0x06:
			stateMu.Lock()
			_, _, _, _, _, _, _, rows := computeLayout()
			col := curCol()
			newRow := curRow() + rows
			maxRow := dataRows() - 1
			if newRow > maxRow {
				newRow = maxRow
			}
			_, _, _, _, _, _, cols, _ := computeLayout()
			idx := newRow*cols + col
			if idx >= len(cands) {
				idx = len(cands) - 1
			}
			moveTo(idx)
			stateMu.Unlock()
			requestRepaint()
			awaitGG = false
		case 0x02:
			stateMu.Lock()
			_, _, _, _, _, _, _, rows := computeLayout()
			col := curCol()
			newRow := curRow() - rows
			if newRow < 0 {
				newRow = 0
			}
			_, _, _, _, _, _, cols, _ := computeLayout()
			idx := newRow*cols + col
			if idx >= len(cands) {
				idx = len(cands) - 1
			}
			moveTo(idx)
			stateMu.Unlock()
			requestRepaint()
			awaitGG = false
		case 'G':
			stateMu.Lock()
			moveTo(len(cands) - 1)
			stateMu.Unlock()
			requestRepaint()
			awaitGG = false
		case 'g':
			if awaitGG {
				stateMu.Lock()
				moveTo(0)
				topRow = 0
				stateMu.Unlock()
				requestRepaint()
				awaitGG = false
			} else {
				awaitGG = true
			}
		case 'k':
			stateMu.Lock()
			_, _, _, _, _, _, cols, _ := computeLayout()
			if cur-cols >= 0 {
				moveTo(cur - cols)
			}
			stateMu.Unlock()
			requestRepaint()
			awaitGG = false
		case 'j':
			stateMu.Lock()
			_, _, _, _, _, _, cols, _ := computeLayout()
			if cur+cols < len(cands) {
				moveTo(cur + cols)
			}
			stateMu.Unlock()
			requestRepaint()
			awaitGG = false
		case 'h':
			stateMu.Lock()
			_, _, _, _, _, _, cols, _ := computeLayout()
			if (cur % cols) > 0 {
				moveTo(cur - 1)
			}
			stateMu.Unlock()
			requestRepaint()
			awaitGG = false
		case 'l':
			stateMu.Lock()
			_, _, _, _, _, _, cols, _ := computeLayout()
			if (cur%cols) < cols-1 && cur+1 < len(cands) {
				moveTo(cur + 1)
			}
			stateMu.Unlock()
			requestRepaint()
			awaitGG = false
		case '+', '=':
			stateMu.Lock()
			zoom++
			stateMu.Unlock()
			requestRepaint()
			awaitGG = false
		case '-', '_':
			stateMu.Lock()
			zoom--
			if zoom < 0 {
				zoom = 0
			}
			stateMu.Unlock()
			requestRepaint()
			awaitGG = false
		case 'p':
			stateMu.Lock()
			showImages = !showImages
			stateMu.Unlock()
			requestRepaint()
			awaitGG = false
		case '\r', '\n':
			out := []string{toAbs(cands[cur].Path)}
			if renderer != nil {
				_ = renderer.ClearAll()
			}
			fmt.Fprint(os.Stdout, "\x1b[2J\x1b[H")
			return out, 0, nil
		default:
			awaitGG = false
		}
	}
}

func otherIcon(path string) string {
	ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(path), "."))
	if ext == "" {
		return "FILE"
	}
	if len(ext) > 4 {
		ext = ext[:4]
	}
	return "[" + ext + "]"
}
