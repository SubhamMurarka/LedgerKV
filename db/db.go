package db

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/subhammurarka/LedgerKV/wal"
)

type IndexEntry struct {
	FileID  int
	Offset  int64
	Size    int64
	Deleted bool
}

type DB struct {
	dir        string
	walMgr     *wal.Manager
	index      map[string]IndexEntry
	mu         sync.RWMutex
	totalBytes int64
	liveBytes  int64
	compacting bool
	stopCh     chan struct{}
}

func Open(dir string) (*DB, error) {
	mgr, err := wal.OpenManager(dir, 1024*1024)
	if err != nil {
		return nil, err
	}

	db := &DB{
		dir:    dir,
		walMgr: mgr,
		index:  make(map[string]IndexEntry),
	}

	files, _ := os.ReadDir(dir)
	for _, f := range files {
		var id int
		fmt.Sscanf(f.Name(), "wal-%06d.log", &id)

		wal.Replay(mgr.Path(id), func(r *wal.Record, off int64) error {
			size := int64(wal.HeaderSize + len(r.Key))
			if r.Value != nil {
				size += int64(len(r.Value))
			}

			db.totalBytes += size

			key := string(r.Key)
			if r.Value == nil {
				db.index[key] = IndexEntry{Deleted: true}
			} else {
				db.index[key] = IndexEntry{
					FileID: id,
					Offset: off,
					Size:   size,
				}
				db.liveBytes += size
			}
			return nil
		})
	}

	db.stopCh = make(chan struct{})
	go db.compactionLoop()

	return db, nil
}

func (db *DB) Put(key, value []byte) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if old, ok := db.index[string(key)]; ok && !old.Deleted {
		db.liveBytes -= old.Size
	}

	fid, off, err := db.walMgr.Append(&wal.Record{Key: key, Value: value})
	if err != nil {
		return err
	}

	size := int64(wal.HeaderSize + len(key) + len(value))
	db.totalBytes += size
	db.liveBytes += size

	db.index[string(key)] = IndexEntry{
		FileID: fid,
		Offset: off,
		Size:   size,
	}

	return nil
}

func (db *DB) Delete(key []byte) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if old, ok := db.index[string(key)]; ok && !old.Deleted {
		db.liveBytes -= old.Size
	}

	db.walMgr.Append(&wal.Record{Key: key})
	db.index[string(key)] = IndexEntry{Deleted: true}
	return nil
}

func (db *DB) Get(key []byte) ([]byte, bool, error) {
	db.mu.RLock()
	entry, ok := db.index[string(key)]
	db.mu.RUnlock()

	if !ok || entry.Deleted {
		return nil, false, nil
	}

	f, err := os.Open(db.walMgr.Path(entry.FileID))
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	f.Seek(entry.Offset, io.SeekStart)
	rec, err := wal.ReadRecord(f)
	if err != nil {
		return nil, false, err
	}

	return rec.Value, true, nil
}

func (db *DB) maybeCompact() {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.compacting || db.totalBytes == 0 {
		return
	}

	garbageRatio :=
		float64(db.totalBytes-db.liveBytes) /
			float64(db.totalBytes)

	if garbageRatio < 0.5 {
		return
	}

	db.compacting = true
	go db.Compact()
}

func (db *DB) compactionLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			db.maybeCompact()
		case <-db.stopCh:
			return
		}
	}
}

func (db *DB) Close() {
	close(db.stopCh)
}
