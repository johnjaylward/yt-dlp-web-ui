package downloaders

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/common"
	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/config"
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

func TestRestoreFromSnapshotReusesDownloadTempPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "downloads")
	d := NewGenericDownload("https://example.com/video", nil).(*GenericDownloader)
	d.output.Path = root
	d.output.Filename = "%(title)s.%(ext)s"

	originalTempPath := d.tempPath(d.output.Path)
	d.output.TempPath = originalTempPath
	if err := os.MkdirAll(originalTempPath, 0750); err != nil {
		t.Fatal(err)
	}
	partialFile := filepath.Join(originalTempPath, "video.part")
	if err := os.WriteFile(partialFile, []byte("partial download"), 0600); err != nil {
		t.Fatal(err)
	}

	snapshot := d.Status()
	restored := NewGenericDownload("", nil).(*GenericDownloader)
	if err := restored.RestoreFromSnapshot(&snapshot); err != nil {
		t.Fatal(err)
	}

	resumedTempPath := restored.tempPath(restored.output.Path)
	if resumedTempPath != originalTempPath {
		t.Fatalf("resumed temp path = %q, want %q", resumedTempPath, originalTempPath)
	}
	if !restored.useIsolatedTemp {
		t.Fatal("restored download did not retain isolated temp-path mode")
	}
	if _, err := os.Stat(filepath.Join(resumedTempPath, "video.part")); err != nil {
		t.Fatalf("partial file is not available at resumed temp path: %v", err)
	}
}

func TestRestoreFromLegacySnapshotKeepsLegacyTempBehavior(t *testing.T) {
	snapshot := &internal.ProcessSnapshot{
		URL: "https://example.com/video",
		Output: internal.DownloadOutput{
			Path:     t.TempDir(),
			Filename: "%(title)s.%(ext)s",
		},
	}

	d := NewGenericDownload("", nil).(*GenericDownloader)
	if err := d.RestoreFromSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if d.useIsolatedTemp {
		t.Fatal("legacy snapshot unexpectedly enabled isolated temp paths")
	}
	if d.output.TempPath != "" {
		t.Fatalf("legacy snapshot temp path = %q, want empty", d.output.TempPath)
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
	configDir := t.TempDir()
	cfg := config.Instance()
	previousConfigPath := cfg.Path()
	cfg.SetPath(filepath.Join(configDir, "config.yml"))
	t.Cleanup(func() { cfg.SetPath(previousConfigPath) })
	archivePath := filepath.Join(cfg.Dir(), "archive.txt")

	params, err := argsSanitizer([]string{
		"--no-mtime",
		"--cookies=cookies.txt",
		"-f",
		"bestvideo+bestaudio",
		"--break-on-existing",
		"--download-archive",
		archivePath,
	})
	if err != nil {
		t.Fatalf("argsSanitizer rejected a built-in UI or subscription argument: %v", err)
	}
	if len(params) != 7 {
		t.Fatalf("argsSanitizer() returned %d arguments, want 7", len(params))
	}
}

func TestCleanupFailedArtifactsRemovesOnlyJobTempDirectory(t *testing.T) {
	root := t.TempDir()
	cfg := config.Instance()
	previousDownloadPath := cfg.Paths.DownloadPath
	cfg.Paths.DownloadPath = root
	t.Cleanup(func() { cfg.Paths.DownloadPath = previousDownloadPath })

	d := NewGenericDownload("https://example.com/video", nil).(*GenericDownloader)
	d.output.Path = root
	d.progress.Status = internal.StatusErrored

	jobTempDir := d.tempPath(root)
	d.output.TempPath = jobTempDir
	if err := os.MkdirAll(jobTempDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobTempDir, "video.part"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	unrelatedFile := filepath.Join(root, "unrelated.part")
	if err := os.WriteFile(unrelatedFile, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := d.CleanupFailedArtifacts(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(jobTempDir); !os.IsNotExist(err) {
		t.Fatalf("job temp directory still exists, stat error = %v", err)
	}
	if _, err := os.Stat(unrelatedFile); err != nil {
		t.Fatalf("unrelated artifact was removed: %v", err)
	}
}

func TestCleanupFailedArtifactsKeepsNonErroredTempDirectory(t *testing.T) {
	root := t.TempDir()
	d := NewGenericDownload("https://example.com/video", nil).(*GenericDownloader)
	d.output.Path = root
	d.progress.Status = internal.StatusCompleted

	jobTempDir := d.tempPath(root)
	if err := os.MkdirAll(jobTempDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := d.CleanupFailedArtifacts(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(jobTempDir); err != nil {
		t.Fatalf("non-errored temp directory was removed: %v", err)
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
