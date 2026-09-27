package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/common"
	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/config"
)

const metadataFetchTimeout = 2 * time.Minute

func DefaultFetcher(url string) (*common.DownloadMetadata, error) {
	ctx, cancel := context.WithTimeout(context.Background(), metadataFetchTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, config.Instance().Paths.DownloaderPath, url, "-J")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	meta := common.DownloadMetadata{
		URL:       url,
		CreatedAt: time.Now(),
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var bufferedStderr bytes.Buffer
	stderrDone := make(chan struct{})

	go func() {
		defer close(stderrDone)
		io.Copy(&bufferedStderr, stderr)
	}()

	slog.Info("retrieving metadata", slog.String("url", url))

	decodeErr := json.NewDecoder(stdout).Decode(&meta)
	waitErr := cmd.Wait()
	<-stderrDone

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("metadata fetch timed out after %s", metadataFetchTimeout)
	}
	if waitErr != nil {
		if stderr := strings.TrimSpace(bufferedStderr.String()); stderr != "" {
			return nil, errors.New(stderr)
		}
		return nil, waitErr
	}
	if decodeErr != nil {
		return nil, decodeErr
	}

	return &meta, nil
}
