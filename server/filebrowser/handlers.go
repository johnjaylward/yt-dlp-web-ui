package filebrowser

import (
	"archive/zip"
	"encoding/base64"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/config"
	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/internal"
	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/internal/kv"
	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/internal/safefs"
)

/*
	File based operation handlers (should be moved to rest/handlers.go) or in
	a entirely self-contained package
*/

var (
	videoRe = regexp.MustCompile(`(?i)/\.mov|\.mp4|\.webm|\.mvk|/gmi`)
)

func isVideo(d fs.DirEntry) bool {
	return videoRe.MatchString(d.Name())
}

func isValidEntry(d fs.DirEntry) bool {
	return !strings.HasPrefix(d.Name(), ".") &&
		!strings.HasSuffix(d.Name(), ".part") &&
		!strings.HasSuffix(d.Name(), ".ytdl")
}

type DirectoryEntry struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	Size        int64     `json:"size"`
	ModTime     time.Time `json:"modTime"`
	IsVideo     bool      `json:"isVideo"`
	IsDirectory bool      `json:"isDirectory"`
}

func walkDir(root *os.Root, subdir string) (*[]DirectoryEntry, error) {
	rel, err := safefs.CleanRelativePath(subdir)
	if err != nil {
		return nil, err
	}
	dir, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	dirs, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}

	var files []DirectoryEntry

	for _, d := range dirs {
		if !isValidEntry(d) {
			continue
		}

		path := filepath.Join(root.Name(), rel, d.Name())

		info, err := d.Info()
		if err != nil {
			return nil, err
		}

		files = append(files, DirectoryEntry{
			Path:        path,
			Name:        d.Name(),
			Size:        info.Size(),
			IsVideo:     isVideo(d),
			IsDirectory: d.IsDir(),
			ModTime:     info.ModTime(),
		})
	}

	return &files, err
}

type ListRequest struct {
	SubDir  string `json:"subdir"`
	OrderBy string `json:"orderBy"`
}

func ListDownloaded(w http.ResponseWriter, r *http.Request) {
	root := config.Instance().Paths.DownloadPath
	fsRoot, err := safefs.OpenDownloadRoot(root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer fsRoot.Close()
	req := new(ListRequest)

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	files, err := walkDir(fsRoot, req.SubDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.OrderBy == "modtime" {
		sort.SliceStable(*files, func(i, j int) bool {
			return (*files)[i].ModTime.After((*files)[j].ModTime)
		})
	}

	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(files); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type DeleteRequest = DirectoryEntry

func DeleteFile(w http.ResponseWriter, r *http.Request) {
	req := new(DeleteRequest)

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	fsRoot, err := safefs.OpenDownloadRoot(config.Instance().Paths.DownloadPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer fsRoot.Close()
	rel, err := safefs.RelativePath(fsRoot.Name(), req.Path)
	if err != nil || rel == "." {
		http.Error(w, "path must identify an entry inside the download directory", http.StatusBadRequest)
		return
	}
	if err := fsRoot.Remove(rel); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode("ok")
}

func SendFile(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "id")

	if path == "" {
		http.Error(w, "inexistent path", http.StatusBadRequest)
		return
	}

	path, err := url.QueryUnescape(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	decoded, err := base64.StdEncoding.DecodeString(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	fsRoot, err := safefs.OpenDownloadRoot(config.Instance().Paths.DownloadPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer fsRoot.Close()
	rel, err := safefs.RelativePath(fsRoot.Name(), string(decoded))
	if err != nil || rel == "." {
		http.Error(w, "path is outside download directory", http.StatusForbidden)
		return
	}
	fd, err := fsRoot.Open(rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	defer fd.Close()
	info, err := fd.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, filepath.Base(rel), info.ModTime(), fd)
}

func DownloadFile(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "id")

	if path == "" {
		http.Error(w, "inexistent path", http.StatusBadRequest)
		return
	}

	path, err := url.QueryUnescape(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	decoded, err := base64.StdEncoding.DecodeString(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	fsRoot, err := safefs.OpenDownloadRoot(config.Instance().Paths.DownloadPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer fsRoot.Close()
	rel, err := safefs.RelativePath(fsRoot.Name(), string(decoded))
	if err != nil || rel == "." {
		http.Error(w, "path is outside download directory", http.StatusForbidden)
		return
	}
	fd, err := fsRoot.Open(rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	defer fd.Close()
	info, err := fd.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(rel)+"\"")
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, filepath.Base(rel), info.ModTime(), fd)
}

func BulkDownload(mdb *kv.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fsRoot, err := safefs.OpenDownloadRoot(config.Instance().Paths.DownloadPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer fsRoot.Close()

		ps := slices.DeleteFunc(*mdb.All(), func(e internal.ProcessSnapshot) bool {
			if e.Progress.Status != internal.StatusCompleted {
				return true
			}
			rel, err := safefs.RelativePath(fsRoot.Name(), e.Output.SavedFilePath)
			return err != nil || rel == "."
		})

		if len(ps) == 0 {
			return
		}

		zipWriter := zip.NewWriter(w)

		w.Header().Add(
			"Content-Disposition",
			"inline; filename=download-archive-"+time.Now().Format(time.RFC3339)+".zip",
		)
		w.Header().Set("Content-Type", "application/zip")

		for _, p := range ps {
			wr, err := zipWriter.Create(filepath.Base(p.Output.SavedFilePath))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			rel, err := safefs.RelativePath(fsRoot.Name(), p.Output.SavedFilePath)
			if err != nil || rel == "." {
				http.Error(w, "saved file is outside download directory", http.StatusForbidden)
				return
			}
			fd, err := fsRoot.Open(rel)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			if _, err := io.Copy(wr, fd); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		if err := zipWriter.Close(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}
