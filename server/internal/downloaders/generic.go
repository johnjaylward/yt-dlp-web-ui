package downloaders

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/google/uuid"
	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/common"
	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/config"
	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/internal"
	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/internal/safefs"
)

const downloadTemplate = `download:
{
	"eta":%(progress.eta)s,
	"percentage":"%(progress._percent_str)s",
	"speed":%(progress.speed)s
}`

// filename not returning the correct extension after postprocess
const postprocessTemplate = `postprocess:
{
	"filepath":"%(info.filepath)s"
}
`

type GenericDownloader struct {
	Params []string

	AutoRemove bool

	progress internal.DownloadProgress
	output   internal.DownloadOutput

	proc *os.Process

	logConsumer     LogConsumer
	useIsolatedTemp bool

	// embedded
	DownloaderBase
}

func NewGenericDownload(url string, params []string) Downloader {
	g := &GenericDownloader{
		logConsumer: NewJSONLogConsumer(),
	}
	// in base
	g.Id = uuid.NewString()
	g.URL = url
	g.Params = params
	g.Completed = false
	g.useIsolatedTemp = true

	return g
}

func (g *GenericDownloader) Start() (startErr error) {
	defer func() {
		if g.progress.Status != internal.StatusCompleted {
			if startErr != nil {
				g.progress.Status = internal.StatusErrored
			} else {
				g.progress.Status = internal.StatusCompleted
			}
		}
		g.Complete()
	}()

	whitelistedParams, err := argsSanitizer(g.Params)
	if err != nil {
		return err
	}

	g.Params = whitelistedParams
	g.SetPending(true)

	out := internal.DownloadOutput{
		Path:     config.Instance().Paths.DownloadPath,
		Filename: "%(title)s.%(ext)s",
	}

	if g.output.Path != "" {
		out.Path = g.output.Path
	}

	if g.output.Filename != "" {
		out.Filename = g.output.Filename
	}

	root, err := safefs.OpenDownloadRoot(config.Instance().Paths.DownloadPath)
	if err != nil {
		return err
	}
	defer root.Close()
	outRel, err := safefs.RelativePath(root.Name(), out.Path)
	if err != nil {
		return err
	}
	if err := root.MkdirAll(outRel, 0750); err != nil {
		return err
	}
	outRoot, err := root.OpenRoot(outRel)
	if err != nil {
		return err
	}
	defer outRoot.Close()
	out.Path = filepath.Join(root.Name(), outRel)

	buildFilename(&out)
	if filepath.IsAbs(out.Filename) || !filepath.IsLocal(out.Filename) ||
		strings.ContainsAny(out.Filename, `/\\`) {
		return errors.New("output filename must not contain a path")
	}
	g.output.Path = out.Path
	g.output.Filename = out.Filename
	tempPath := g.output.TempPath
	if g.useIsolatedTemp {
		if tempPath == "" {
			tempPath = g.tempPath(out.Path)
			g.output.TempPath = tempPath
		} else {
			var tempRel string
			tempRel, err = safefs.RelativePath(out.Path, tempPath)
			if err != nil || tempRel == "." {
				return errors.New("temporary directory must be inside the output directory")
			}
			tempPath = filepath.Join(out.Path, tempRel)
		}
		tempRel, err := safefs.RelativePath(out.Path, tempPath)
		if err != nil || tempRel == "." {
			return errors.New("temporary directory must be inside the output directory")
		}
		if err := outRoot.MkdirAll(tempRel, 0750); err != nil {
			return err
		}
		if tempRoot, err := outRoot.OpenRoot(tempRel); err != nil {
			return err
		} else {
			tempRoot.Close()
		}
	}

	templateReplacer := strings.NewReplacer("\n", "", "\t", "", " ", "")

	baseParams := []string{
		strings.Split(g.URL, "?list")[0], // no playlist
		"--newline",
		"--no-colors",
		"--no-playlist",
		"--progress-template",
		templateReplacer.Replace(downloadTemplate),
		"--progress-template",
		templateReplacer.Replace(postprocessTemplate),
		"--no-exec",
		"--paths",
		"temp:" + tempPath,
		"--js-runtimes",
		config.Instance().Paths.JSRuntimePath,
		"--remote-components",
		"ejs:github",
	}
	if g.useIsolatedTemp {
		baseParams = append(baseParams, "--paths", "home:"+out.Path, "--paths", "temp:"+tempPath)
	}

	// if user asked to manually override the output path...
	outputPath := filepath.Join(out.Path, out.Filename)
	outputRel, err := safefs.RelativePath(root.Name(), outputPath)
	if err != nil {
		return errors.New(ErrIsNotSubPath)
	}
	if parent := filepath.Dir(outputRel); parent != "." {
		if checkRoot, err := root.OpenRoot(parent); err != nil {
			return err
		} else {
			checkRoot.Close()
		}
	}
	outputTemplate := outputPath
	if g.useIsolatedTemp {
		outputTemplate = out.Filename
	}
	g.Params = append(g.Params, "-o", outputTemplate)

	params := append(baseParams, g.Params...)

	slog.Info("requesting download",
		slog.String("id", g.Id),
		slog.String("url", g.URL),
		slog.Any("params", params),
	)

	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, config.Instance().Paths.DownloaderPath, params...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		slog.Error("failed to get a stdout pipe", slog.Any("err", err))
		return err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		slog.Error("failed to get a stderr pipe", slog.Any("err", err))
		return err
	}

	if err := cmd.Start(); err != nil {
		slog.Error("failed to start yt-dlp process", slog.Any("err", err))
		return err
	}

	g.proc = cmd.Process
	g.SetProgress(internal.DownloadProgress{Status: internal.StatusDownloading})

	defer func() {
		stdout.Close()
		cancel()
	}()

	logs := make(chan []byte)
	go produceLogs(stdout, logs)
	go consumeLogs(ctx, logs, g.logConsumer, g)

	go printYtDlpErrors(stderr, g.Id, g.URL)

	g.SetPending(false)
	err = cmd.Wait()
	if err == nil && g.useIsolatedTemp {
		tempRel, relErr := safefs.RelativePath(out.Path, tempPath)
		if relErr == nil {
			if cleanupErr := outRoot.RemoveAll(tempRel); cleanupErr != nil {
				slog.Warn("failed to remove download temp directory",
					slog.String("id", g.Id),
					slog.String("path", tempPath),
					slog.Any("err", cleanupErr),
				)
			}
		} else {
			slog.Warn("refusing to remove temp directory outside output root", slog.String("path", tempPath), slog.Any("err", relErr))
		}
	}
	return err
}

func (g *GenericDownloader) tempPath(root string) string {
	idHash := sha256.Sum256([]byte(g.Id))
	return filepath.Join(root, ".yt-dlp-webui-temp", hex.EncodeToString(idHash[:]))
}

func (g *GenericDownloader) CleanupFailedArtifacts() error {
	if g.progress.Status != internal.StatusErrored || g.output.TempPath == "" {
		return nil
	}

	root := g.output.Path
	if root == "" {
		root = config.Instance().Paths.DownloadPath
	}
	fsRoot, err := safefs.OpenDownloadRoot(config.Instance().Paths.DownloadPath)
	if err != nil {
		return err
	}
	defer fsRoot.Close()
	rootRel, err := safefs.RelativePath(fsRoot.Name(), root)
	if err != nil {
		return err
	}
	outRoot, err := fsRoot.OpenRoot(rootRel)
	if err != nil {
		return err
	}
	defer outRoot.Close()
	tempRel, err := safefs.RelativePath(root, g.output.TempPath)
	if err != nil || tempRel == "." {
		return errors.New("temporary directory is outside output root")
	}
	return outRoot.RemoveAll(tempRel)
}

func (g *GenericDownloader) Stop() error {
	defer func() {
		g.progress.Status = internal.StatusCompleted
		g.Complete()
	}()
	// yt-dlp uses multiple child process the parent process
	// has been spawned with setPgid = true. To properly kill
	// all subprocesses a SIGTERM need to be sent to the correct
	// process group
	if g.proc == nil {
		return errors.New("*os.Process not set")
	}

	pgid, err := syscall.Getpgid(g.proc.Pid)
	if err != nil {
		return err
	}
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
		return err
	}

	return nil
}

func (g *GenericDownloader) Status() internal.ProcessSnapshot {
	return internal.ProcessSnapshot{
		Id:             g.Id,
		URL:            g.URL,
		Completed:      g.Completed,
		Info:           g.Metadata,
		Progress:       g.progress,
		Output:         g.output,
		Params:         g.Params,
		DownloaderName: "generic",
	}
}

func (g *GenericDownloader) UpdateSavedFilePath(p string) { g.output.SavedFilePath = p }

func (g *GenericDownloader) SetOutput(o internal.DownloadOutput)     { g.output = o }
func (g *GenericDownloader) SetProgress(p internal.DownloadProgress) { g.progress = p }

func (g *GenericDownloader) SetMetadata(fetcher func(url string) (*common.DownloadMetadata, error)) {
	g.FetchMetadata(fetcher)
}

func (g *GenericDownloader) SetPending(p bool) {
	g.Pending = p
}

func (g *GenericDownloader) GetId() string  { return g.Id }
func (g *GenericDownloader) GetUrl() string { return g.URL }

func (g *GenericDownloader) RestoreFromSnapshot(snap *internal.ProcessSnapshot) error {
	if snap == nil {
		return errors.New("cannot restore nil snapshot")
	}

	s := *snap

	g.Id = s.Id
	g.URL = s.URL
	if g.URL == "" {
		g.URL = s.Info.URL
	}
	g.Metadata = s.Info
	g.Completed = s.Completed || s.Progress.Status == internal.StatusCompleted
	g.progress = s.Progress
	g.output = s.Output
	g.useIsolatedTemp = s.Output.TempPath != ""
	g.Params = s.Params

	return nil
}

func (g *GenericDownloader) IsCompleted() bool { return g.Completed }
