package wal

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
)

const HeaderSize = 12

type Record struct {
	Key   []byte
	Value []byte // nil => tombstone
}

func (r *Record) computeCRC() uint32 {
	buf := new(bytes.Buffer)

	_ = binary.Write(buf, binary.LittleEndian, uint32(len(r.Key)))

	var valueSize int32 = -1
	if r.Value != nil {
		valueSize = int32(len(r.Value))
	}
	_ = binary.Write(buf, binary.LittleEndian, valueSize)

	buf.Write(r.Key)
	if r.Value != nil {
		buf.Write(r.Value)
	}

	return crc32.ChecksumIEEE(buf.Bytes())
}
