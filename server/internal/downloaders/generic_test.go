package downloaders

import (
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
