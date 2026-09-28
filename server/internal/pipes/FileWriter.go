package pipes

import (
	"io"
	"log/slog"
	"os"

	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/config"
	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/internal/safefs"
)

type FileWriter struct {
	Path    string
	IsFinal bool
}

func (f *FileWriter) Name() string { return "file-writer" }

func (f *FileWriter) Connect(r io.Reader) (io.Reader, error) {
	root, err := safefs.OpenDownloadRoot(config.Instance().Paths.DownloadPath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	rel, err := safefs.RelativePath(root.Name(), f.Path)
	if err != nil || rel == "." {
		return nil, os.ErrPermission
	}
	file, err := root.OpenFile(rel, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
	if err != nil {
		return nil, err
	}

	if f.IsFinal {
		go func() {
			defer file.Close()
			if _, err := io.Copy(file, r); err != nil {
				slog.Error("FileWriter (final) error", slog.Any("err", err))
			}
		}()
		return r, nil
	}

	pr, pw := io.Pipe()

	go func() {
		defer file.Close()
		defer pw.Close()

		writer := io.MultiWriter(file, pw)
		if _, err := io.Copy(writer, r); err != nil {
			slog.Error("FileWriter (pipeline) error", slog.Any("err", err))
		}
	}()

	return pr, nil
}
