package thumb

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const cacheVersion = "ffmpeg-v1"

func debugf(format string, a ...any) {
	if os.Getenv("THUMBGRID_DEBUG") == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "thumbgrid: "+format+"\n", a...)
}

func Generate(path string, size int, cacheDir string) (string, error) {
	abs := path
	if !filepath.IsAbs(abs) {
		a, _ := filepath.Abs(path)
		abs = a
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	key := cacheKey(abs, size, info.ModTime(), info.Size())
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	out := filepath.Join(cacheDir, key+".png")
	if _, err := os.Stat(out); err == nil {
		debugf("cache hit (square): %s", out)
		return out, nil
	}

	if isVideo(abs) && hasExec("ffmpeg") && strings.ToLower(os.Getenv("THUMBGRID_VIDEO_TOOL")) != "magick" {
		f, _ := os.CreateTemp(cacheDir, "thumbgrid.*.png")
		tmp := f.Name()
		_ = f.Close()
		if runErr := ffmpegGrab(abs, size, size, tmp); runErr == nil {
			debugf("video via ffmpeg size=%d: %s", size, abs)
			_ = os.Rename(tmp, out)
			return out, nil
		} else {
			debugf("ffmpeg (square) failed: %v", runErr)
			_ = os.Remove(tmp)
		}
	}

	if !isVideo(abs) && hasExec("vipsthumbnail") && strings.ToLower(os.Getenv("THUMBGRID_IMAGE_TOOL")) != "magick" {
		f, _ := os.CreateTemp(cacheDir, "thumbgrid.*.png")
		tmp := f.Name()
		_ = f.Close()
		cmd := exec.Command("vipsthumbnail", abs, "-s", strconv.Itoa(size), "-o", tmp)
		if runErr := cmd.Run(); runErr == nil {
			debugf("image via vipsthumbnail size=%d: %s", size, abs)
			_ = os.Rename(tmp, out)
			return out, nil
		} else {
			debugf("vipsthumbnail failed: %v", runErr)
		}
		_ = os.Remove(tmp)
	}

	if hasExec("magick") {
		f, _ := os.CreateTemp(cacheDir, "thumbgrid.*.png")
		tmp := f.Name()
		_ = f.Close()
		cmd := exec.Command(
			"magick",
			abs+srcFrameSuffix(abs),
			"-thumbnail", fmt.Sprintf("%dx%d", size, size),
			"-background", "none",
			"-gravity", "center",
			"-extent", fmt.Sprintf("%dx%d", size, size),
			tmp,
		)
		if runErr := cmd.Run(); runErr == nil {
			debugf("square via magick size=%d: %s", size, abs)
			_ = os.Rename(tmp, out)
			return out, nil
		} else {
			debugf("magick (square) failed: %v", runErr)
		}
		_ = os.Remove(tmp)
	}

	return "", fmt.Errorf("no image tool available (install ffmpeg, vipsthumbnail, or magick)")
}

func hasExec(name string) bool { _, err := exec.LookPath(name); return err == nil }

func cacheKey(path string, size int, mt time.Time, fsz int64) string {
	h := sha1.New()
	io.WriteString(h, path)
	io.WriteString(h, "|")
	io.WriteString(h, strconv.Itoa(size))
	io.WriteString(h, "|")
	io.WriteString(h, strconv.FormatInt(mt.Unix(), 10))
	io.WriteString(h, "|")
	io.WriteString(h, strconv.FormatInt(fsz, 10))
	io.WriteString(h, "|")
	io.WriteString(h, cacheVersion)
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)
}

func GenerateRect(path string, w, h int, cacheDir string) (string, error) {
	if w <= 0 || h <= 0 {
		return Generate(path, max(w, h), cacheDir)
	}
	abs := path
	if !filepath.IsAbs(abs) {
		a, _ := filepath.Abs(path)
		abs = a
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	key := cacheKeyRect(abs, w, h, info.ModTime(), info.Size())
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	out := filepath.Join(cacheDir, key+".png")
	if _, err := os.Stat(out); err == nil {
		debugf("cache hit (rect): %s", out)
		return out, nil
	}

	if isVideo(abs) && hasExec("ffmpeg") && strings.ToLower(os.Getenv("THUMBGRID_VIDEO_TOOL")) != "magick" {
		f, _ := os.CreateTemp(cacheDir, "thumbgrid.*.png")
		tmp := f.Name()
		_ = f.Close()
		if runErr := ffmpegGrab(abs, w, h, tmp); runErr == nil {
			debugf("video via ffmpeg size=%dx%d: %s", w, h, abs)
			_ = os.Rename(tmp, out)
			return out, nil
		} else {
			debugf("ffmpeg (rect) failed: %v", runErr)
			_ = os.Remove(tmp)
		}
	}
	if hasExec("magick") {
		f, _ := os.CreateTemp(cacheDir, "thumbgrid.*.png")
		tmp := f.Name()
		_ = f.Close()
		cmd := exec.Command(
			"magick",
			abs+srcFrameSuffix(abs),
			"-thumbnail", fmt.Sprintf("%dx%d", w, h),
			"-background", "none",
			"-gravity", "center",
			"-extent", fmt.Sprintf("%dx%d", w, h),
			tmp,
		)
		if runErr := cmd.Run(); runErr == nil {
			debugf("rect via magick %dx%d: %s", w, h, abs)
			_ = os.Rename(tmp, out)
			return out, nil
		} else {
			debugf("magick (rect) failed: %v", runErr)
		}
		_ = os.Remove(tmp)
	}
	return Generate(path, max(w, h), cacheDir)
}

func cacheKeyRect(path string, w, h int, mt time.Time, fsz int64) string {
	hsh := sha1.New()
	io.WriteString(hsh, path)
	io.WriteString(hsh, "|")
	io.WriteString(hsh, strconv.Itoa(w))
	io.WriteString(hsh, "x")
	io.WriteString(hsh, strconv.Itoa(h))
	io.WriteString(hsh, "|")
	io.WriteString(hsh, strconv.FormatInt(mt.Unix(), 10))
	io.WriteString(hsh, "|")
	io.WriteString(hsh, strconv.FormatInt(fsz, 10))
	io.WriteString(hsh, "|")
	io.WriteString(hsh, cacheVersion)
	return hex.EncodeToString(hsh.Sum(nil))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func srcFrameSuffix(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp4", ".mov", ".mkv", ".webm", ".avi", ".m4v":
		return "[0]"
	default:
		return ""
	}
}

func isVideo(path string) bool {
	return srcFrameSuffix(path) != ""
}

func ffmpegGrab(abs string, w, h int, out string) error {
	if w <= 0 || h <= 0 {

		size := max(w, h)
		if size <= 0 {
			size = 256
		}
		w, h = size, size
	}

	seek := 2.0
	if hasExec("ffprobe") {
		if dur, err := probeDuration(abs); err == nil && dur > 0.0 {
			s := dur * 0.10
			if s < 0.5 {
				s = 0.5
			}
			if s > dur-0.1 {
				s = dur - 0.1
			}
			seek = s
		}
	}
	seekStr := fmt.Sprintf("%.3f", seek)

	vf := fmt.Sprintf(
		"scale=%d:%d:force_original_aspect_ratio=decrease,"+
			"pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=black@0,format=rgba",
		w, h, w, h,
	)
	cmd := exec.Command(
		"ffmpeg",
		"-v", "error",
		"-ss", seekStr,
		"-i", abs,
		"-frames:v", "1",
		"-vf", vf,
		"-y", out,
	)
	return cmd.Run()
}

func probeDuration(abs string) (float64, error) {
	cmd := exec.Command(
		"ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "format=duration",
		"-of", "default=nokey=1:noprint_wrappers=1",
		abs,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(out))
	if s == "" || s == "N/A" {
		return 0, fmt.Errorf("no duration")
	}
	d, perr := strconv.ParseFloat(s, 64)
	if perr != nil || !(d > 0) {
		return 0, fmt.Errorf("bad duration: %q", s)
	}
	return d, nil
}
