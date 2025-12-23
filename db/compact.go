package db

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/subhammurarka/LedgerKV/wal"
)

func (db *DB) Compact() error {
	db.mu.Lock()
	db.walMgr.Rotate()
	activeID := db.walMgr.ActiveID()
	db.mu.Unlock()

	tmpPath := filepath.Join(db.dir, "wal-compact.tmp")
	compactWAL, err := wal.OpenWAL(tmpPath)
	if err != nil {
		return err
	}

	entries, _ := os.ReadDir(db.dir)
	for _, e := range entries {
		var fid int
		if _, err := fmt.Sscanf(e.Name(), "wal-%06d.log", &fid); err != nil {
			continue
		}
		if fid >= activeID {
			continue
		}

		f, _ := os.Open(db.walMgr.Path(fid))
		for {
			offset, _ := f.Seek(0, io.SeekCurrent)
			rec, err := wal.ReadRecord(f)
			if err != nil {
				break
			}

			key := string(rec.Key)
			db.mu.RLock()
			entry, ok := db.index[key]
			db.mu.RUnlock()

			if !ok || entry.Deleted || entry.FileID != fid || entry.Offset != offset {
				continue
			}

			newOffset, _ := compactWAL.Append(rec)

			db.mu.Lock()
			db.index[key] = IndexEntry{
				FileID: activeID + 1,
				Offset: newOffset,
				Size:   entry.Size,
			}
			db.mu.Unlock()
		}
		f.Close()
	}

	compactWAL.File().Sync()
	finalPath := db.walMgr.Path(activeID + 1)
	os.Rename(tmpPath, finalPath)

	for _, e := range entries {
		var fid int
		fmt.Sscanf(e.Name(), "wal-%06d.log", &fid)
		if fid < activeID {
			os.Remove(db.walMgr.Path(fid))
		}
	}

	db.mu.Lock()
	db.totalBytes = db.liveBytes
	db.compacting = false
	db.mu.Unlock()

	return nil
}
