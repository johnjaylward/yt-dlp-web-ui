package downloaders

import (
	"log/slog"
	"sync"
	"time"

	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/common"
)

type DownloaderBase struct {
	Id        string
	URL       string
	Metadata  common.DownloadMetadata
	Pending   bool
	Completed bool
	mutex     sync.Mutex
}

func (d *DownloaderBase) FetchMetadata(fetcher func(url string) (*common.DownloadMetadata, error)) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	startedAt := time.Now()
	slog.Info("retrieving metadata",
		slog.String("id", d.Id),
		slog.String("url", d.URL),
	)

	meta, err := fetcher(d.URL)
	if err != nil {
		slog.Error("failed to retrieve metadata",
			slog.String("id", d.Id),
			slog.String("url", d.URL),
			slog.Duration("duration", time.Since(startedAt)),
			slog.Any("err", err),
		)
		return
	}

	d.Metadata = *meta
	slog.Info("metadata retrieved",
		slog.String("id", d.Id),
		slog.String("url", d.URL),
		slog.Duration("duration", time.Since(startedAt)),
	)
}

func (d *DownloaderBase) SetPending(p bool) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.Pending = p
}

func (d *DownloaderBase) Complete() {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.Completed = true
}
