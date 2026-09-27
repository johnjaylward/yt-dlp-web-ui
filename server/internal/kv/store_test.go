package kv

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/internal"
	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/internal/downloaders"
	bolt "go.etcd.io/bbolt"
)

func TestRestoreDeletesSnapshotWithEmptyURL(t *testing.T) {
	db, err := bolt.Open(filepath.Join(t.TempDir(), "bolt.db"), 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	const id = "empty-url-download"
	data, err := json.Marshal(internal.ProcessSnapshot{
		Id:             id,
		DownloaderName: "generic",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(bucket)
		if err != nil {
			return err
		}
		return b.Put([]byte(id), data)
	}); err != nil {
		t.Fatal(err)
	}

	store := &Store{db: db, table: make(map[string]downloaders.Downloader)}
	store.Restore(nil)

	if err := db.View(func(tx *bolt.Tx) error {
		if tx.Bucket(bucket).Get([]byte(id)) != nil {
			t.Errorf("snapshot %q was not deleted", id)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
