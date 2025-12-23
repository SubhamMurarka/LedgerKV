package wal

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
)

func readFull(r io.Reader, buf []byte) error {
	_, err := io.ReadFull(r, buf)
	return err
}

func ReadRecord(f *os.File) (*Record, error) {
	header := make([]byte, HeaderSize)
	if err := readFull(f, header); err != nil {
		return nil, err
	}

	keySize := binary.LittleEndian.Uint32(header[0:4])
	valueSize := int32(binary.LittleEndian.Uint32(header[4:8]))
	expectedCRC := binary.LittleEndian.Uint32(header[8:12])

	key := make([]byte, keySize)
	if err := readFull(f, key); err != nil {
		return nil, err
	}

	var value []byte
	if valueSize >= 0 {
		value = make([]byte, valueSize)
		if err := readFull(f, value); err != nil {
			return nil, err
		}
	}

	rec := &Record{Key: key, Value: value}
	if rec.computeCRC() != expectedCRC {
		return nil, errors.New("crc mismatch")
	}

	return rec, nil
}

func Replay(path string, fn func(*Record, int64) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	for {
		offset, _ := f.Seek(0, io.SeekCurrent)
		rec, err := ReadRecord(f)
		if err != nil {
			return nil
		}
		if err := fn(rec, offset); err != nil {
			return err
		}
	}
}
