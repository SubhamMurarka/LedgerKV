package wal

import (
	"encoding/binary"
	"os"
	"sync"
)

type WAL struct {
	file *os.File
	mu   sync.Mutex
}

func OpenWAL(path string) (*WAL, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &WAL{file: f}, nil
}

func (w *WAL) Append(rec *Record) (int64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	offset, err := w.file.Seek(0, os.SEEK_END)
	if err != nil {
		return 0, err
	}

	keySize := uint32(len(rec.Key))
	var valueSize int32 = -1
	if rec.Value != nil {
		valueSize = int32(len(rec.Value))
	}

	crc := rec.computeCRC()

	header := make([]byte, 12)
	binary.LittleEndian.PutUint32(header[0:4], keySize)
	binary.LittleEndian.PutUint32(header[4:8], uint32(valueSize))
	binary.LittleEndian.PutUint32(header[8:12], crc)

	if _, err := w.file.Write(header); err != nil {
		return 0, err
	}

	if rec.Value != nil {
		data := make([]byte, 0, len(rec.Key)+len(rec.Value))
		data = append(data, rec.Key...)
		data = append(data, rec.Value...)

		if _, err := w.file.Write(data); err != nil {
			return 0, err
		}
	} else {
		if _, err := w.file.Write(rec.Key); err != nil {
			return 0, err
		}
	}

	if err := w.file.Sync(); err != nil {
		return 0, err
	}

	return offset, nil
}

func (w *WAL) Path() string {
	return w.file.Name()
}

func (w *WAL) File() *os.File {
	return w.file
}
