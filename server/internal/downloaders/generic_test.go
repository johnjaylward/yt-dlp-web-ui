package downloaders

import (
	"os"
	"os/exec"
	"regexp"
	"sort"
	"testing"

	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/common"
	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/internal"
)

func TestSnapshotPreservesURLAndCompletion(t *testing.T) {
	const url = "https://example.com/video"
	d := NewGenericDownload(url, nil).(*GenericDownloader)
	d.Completed = true

	snapshot := d.Status()
	if snapshot.URL != url {
		t.Fatalf("snapshot URL = %q, want %q", snapshot.URL, url)
	}
	if !snapshot.Completed {
		t.Fatal("snapshot did not preserve completion state")
	}
}

func TestRestoreFromSnapshotUsesURLAndCompletion(t *testing.T) {
	const url = "https://example.com/video"
	d := &GenericDownloader{}
	snapshot := &internal.ProcessSnapshot{
		URL:       url,
		Completed: true,
		Info:      common.DownloadMetadata{URL: "https://example.com/canonical-video"},
	}

	if err := d.RestoreFromSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if d.GetUrl() != url {
		t.Fatalf("restored URL = %q, want %q", d.GetUrl(), url)
	}
	if !d.IsCompleted() {
		t.Fatal("restored download is not marked completed")
	}
}

func TestRestoreFromLegacySnapshotFallsBackToMetadataURL(t *testing.T) {
	const url = "https://example.com/video"
	d := &GenericDownloader{}
	snapshot := &internal.ProcessSnapshot{
		Info: common.DownloadMetadata{URL: url},
	}

	if err := d.RestoreFromSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if d.GetUrl() != url {
		t.Fatalf("restored URL = %q, want %q", d.GetUrl(), url)
	}
}

func TestStartMarksRejectedArgumentsAsErrored(t *testing.T) {
	d := NewGenericDownload("https://example.com/video", []string{"--not-allowed"}).(*GenericDownloader)

	if err := d.Start(); err == nil {
		t.Fatal("Start() returned nil for an unsupported argument")
	}
	if !d.IsCompleted() {
		t.Fatal("failed download was not marked completed")
	}
	if d.progress.Status != internal.StatusErrored {
		t.Fatalf("progress status = %d, want %d", d.progress.Status, internal.StatusErrored)
	}
}

func TestArgsSanitizerAllowsDefaultFrontendArgument(t *testing.T) {
	params, err := argsSanitizer([]string{
		"--no-mtime",
		"--cookies=cookies.txt",
		"-f",
		"bestvideo+bestaudio",
		"--break-on-existing",
		"--download-archive",
		"/config/archive.txt",
	})
	if err != nil {
		t.Fatalf("argsSanitizer rejected a built-in UI or subscription argument: %v", err)
	}
	if len(params) != 7 {
		t.Fatalf("argsSanitizer() returned %d arguments, want 7", len(params))
	}
}

func TestAllowedFlagsSupportedByYtDlp(t *testing.T) {
	var help []byte
	var err error
	if helpPath := os.Getenv("YTDLP_HELP_PATH"); helpPath != "" {
		help, err = os.ReadFile(helpPath)
		if err != nil {
			t.Fatalf("read packaged yt-dlp help from %q: %v", helpPath, err)
		}
	} else {
		path := os.Getenv("YTDLP_PATH")
		if path == "" {
			path = "yt-dlp"
		}

		if _, err := exec.LookPath(path); err != nil {
			if os.Getenv("YTDLP_REQUIRED") == "1" {
				t.Fatalf("required yt-dlp executable %q not found: %v", path, err)
			}
			t.Skipf("yt-dlp executable %q not found", path)
		}
		help, err = exec.Command(path, "--help").CombinedOutput()
		if err != nil {
			t.Fatalf("read yt-dlp help: %v\n%s", err, help)
		}
	}

	var unsupported []string
	for flag := range allowedFlags {
		pattern := regexp.MustCompile(`(?m)(?:^|[\s,])` + regexp.QuoteMeta(flag) + `(?:$|[\s,=])`)
		if !pattern.Match(help) {
			unsupported = append(unsupported, flag)
		}
	}

	if len(unsupported) > 0 {
		sort.Strings(unsupported)
		t.Fatalf("yt-dlp does not advertise allowlisted flags: %v", unsupported)
	}
}
