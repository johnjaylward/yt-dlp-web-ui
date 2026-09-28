package downloaders

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/config"
	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/internal"
)

// true means the option requires a value; false means it is boolean.
var allowedFlags = map[string]bool{
	"-f":                               true, // format selection
	"--format":                         true,
	"-S":                               true,
	"--format-sort":                    true,
	"--format-sort-reset":              false,
	"--format-sort-force":              false,
	"--no-format-sort-force":           false,
	"--video-multistreams":             false,
	"--no-video-multistreams":          false,
	"--audio-multistreams":             false,
	"--no-audio-multistreams":          false,
	"--prefer-free-formats":            false,
	"--no-prefer-free-formats":         false,
	"--check-formats":                  false,
	"--check-all-formats":              false,
	"--no-check-formats":               false,
	"-F":                               false,
	"--list-formats":                   false,
	"--merge-output-format":            true,
	"--no-mtime":                       false,
	"-I":                               true, // video / playlist selection
	"--playlist-items":                 true,
	"--min-filesize":                   true,
	"--max-filesize":                   true,
	"--date":                           true,
	"--datebefore":                     true,
	"--dateafter":                      true,
	"--match-filters":                  true,
	"--no-match-filters":               false,
	"--break-match-filters":            true,
	"--no-break-match-filters":         false,
	"--yes-playlist":                   false,
	"--age-limit":                      true,
	"--max-downloads":                  true,
	"--playlist-random":                false,
	"--break-on-existing":              false,
	"--download-archive":               true,
	"-N":                               true, // download behaviour
	"--concurrent-fragments":           true,
	"-r":                               true,
	"--limit-rate":                     true,
	"--throttled-rate":                 true,
	"-R":                               true,
	"--retries":                        true,
	"--fragment-retries":               true,
	"--retry-sleep":                    true,
	"--skip-unavailable-fragments":     false,
	"--abort-on-unavailable-fragments": false,
	"--buffer-size":                    true,
	"--resize-buffer":                  false,
	"--no-resize-buffer":               false,
	"--http-chunk-size":                true,
	"--download-sections":              true,
	"--hls-use-mpegts":                 false,
	"--no-hls-use-mpegts":              false,
	"--write-subs":                     false, // subs
	"--no-write-subs":                  false,
	"--write-auto-subs":                false,
	"--no-write-auto-subs":             false,
	"--list-subs":                      false,
	"--sub-format":                     true,
	"--sub-langs":                      true,
	"--convert-subs":                   true,
	"--cookies":                        true,
	"--write-thumbnail":                false, // thumbs
	"--write-all-thumbnails":           false,
	"--convert-thumbnails":             true,
	"--embed-subs":                     false,
	"--embed-thumbnail":                false,
	"--embed-metadata":                 false,
	"--embed-chapters":                 false,
	"--embed-info-json":                false,
	"--write-description":              false, // meta
	"--audio-format":                   true,  // safe post processing
	"--audio-quality":                  true,
	"--remux-video":                    true,
	"--recode-video":                   true,
	"--split-chapters":                 false,
	"--no-split-chapters":              false,
	"--remove-chapters":                true,
	"--no-remove-chapters":             false,
	"--force-keyframes-at-cuts":        false,
	"--no-force-keyframes-at-cuts":     false,
	"--fixup":                          true,
	"--sponsorblock-mark":              true, // sponsorblock
	"--sponsorblock-remove":            true,
	"--sponsorblock-chapter-title":     true,
	"--no-sponsorblock":                false,
	"-v":                               false, // verbosity
	"--verbose":                        false,
}

// SanitizeArgs validates the allowlisted yt-dlp options and their argument
// arity. Positional arguments are rejected because URLs are supplied separately.
func SanitizeArgs(params []string) ([]string, error) {
	if len(params) > 128 {
		return nil, fmt.Errorf("too many downloader arguments")
	}
	var out []string

	for i := 0; i < len(params); i++ {
		p := params[i]
		if p == "" || len(p) > 4096 {
			return nil, fmt.Errorf("downloader argument is empty or too long")
		}
		if strings.Contains(p, "${") || strings.Contains(p, "&&") {
			return nil, fmt.Errorf("downloader argument contains a disallowed sequence")
		}
		if !strings.HasPrefix(p, "-") {
			return nil, fmt.Errorf("unexpected positional downloader argument %q", p)
		}

		flag, inlineValue, hasInlineValue := strings.Cut(p, "=")
		takesValue, allowed := allowedFlags[flag]
		if !allowed {
			return nil, fmt.Errorf("param %s not allowed", p)
		}
		if !takesValue {
			if hasInlineValue {
				return nil, fmt.Errorf("boolean option %s does not accept a value", flag)
			}
			out = append(out, p)
			continue
		}

		value := inlineValue
		if !hasInlineValue {
			if i+1 >= len(params) {
				return nil, fmt.Errorf("option %s requires a value", flag)
			}
			i++
			value = params[i]
			valueFlag := strings.SplitN(value, "=", 2)[0]
			_, isAllowedFlag := allowedFlags[valueFlag]
			if isAllowedFlag || strings.HasPrefix(value, "--") {
				return nil, fmt.Errorf("option %s requires a value before the next option", flag)
			}
		}
		if value == "" || len(value) > 4096 || strings.Contains(value, "${") || strings.Contains(value, "&&") {
			return nil, fmt.Errorf("invalid value for option %s", flag)
		}

		switch flag {
		case "--cookies":
			if value != "cookies.txt" {
				return nil, fmt.Errorf("--cookies may only use the configured cookies.txt file")
			}
		case "--download-archive":
			archivePath := filepath.Join(config.Instance().Dir(), "archive.txt")
			if value != archivePath {
				return nil, fmt.Errorf("--download-archive may only use the configured archive file")
			}
		}

		if hasInlineValue {
			out = append(out, p)
		} else {
			out = append(out, flag, value)
		}
	}

	return out, nil
}

func argsSanitizer(params []string) ([]string, error) { return SanitizeArgs(params) }

func buildFilename(o *internal.DownloadOutput) {
	if o.Filename != "" && strings.Contains(o.Filename, ".%(ext)s") {
		o.Filename += ".%(ext)s"
	}

	o.Filename = strings.Replace(
		o.Filename,
		".%(ext)s.%(ext)s",
		".%(ext)s",
		1,
	)
}

func produceLogs(r io.Reader, logs chan<- []byte) {
	go func() {
		scanner := bufio.NewScanner(r)

		for scanner.Scan() {
			logs <- scanner.Bytes()
		}
	}()
}

func consumeLogs(ctx context.Context, logs <-chan []byte, c LogConsumer, d Downloader) {
	for {
		select {
		case <-ctx.Done():
			slog.Info("detaching from yt-dlp log buffer",
				slog.String("url", d.GetUrl()),
				slog.String("id", c.GetName()),
			)
			return
		case entry := <-logs:
			c.ParseLogEntry(entry, d)
		}
	}
}

func printYtDlpErrors(stdout io.Reader, shortId, url string) {
	scanner := bufio.NewScanner(stdout)

	for scanner.Scan() {
		slog.Error("yt-dlp process error",
			slog.String("id", shortId),
			slog.String("url", url),
			slog.String("err", scanner.Text()),
		)
	}
}
