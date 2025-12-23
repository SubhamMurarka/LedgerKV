package wal

import (
	"fmt"
	"os"
	"path/filepath"
)

type Manager struct {
	dir      string
	active   *WAL
	activeID int
	nextID   int
	maxSize  int64
}

func OpenManager(dir string, maxSize int64) (*Manager, error) {
	os.MkdirAll(dir, 0755)

	m := &Manager{dir: dir, maxSize: maxSize}
	files, _ := filepath.Glob(filepath.Join(dir, "wal-*.log"))

	maxID := 0
	for _, f := range files {
		var id int
		fmt.Sscanf(filepath.Base(f), "wal-%06d.log", &id)
		if id > maxID {
			maxID = id
		}
	}

	m.nextID = maxID + 1
	m.activeID = m.nextID
	m.nextID++

	if err := m.rotateActive(); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *Manager) rotateActive() error {
	path := m.Path(m.activeID)
	w, err := OpenWAL(path)
	if err != nil {
		return err
	}
	m.active = w
	return nil
}

func (m *Manager) Rotate() error {
	m.activeID = m.nextID
	m.nextID++
	return m.rotateActive()
}

func (m *Manager) ReserveID() int {
	id := m.nextID
	m.nextID++
	return id
}

func (m *Manager) Append(rec *Record) (int, int64, error) {
	offset, err := m.active.Append(rec)
	if err != nil {
		return 0, 0, err
	}

	info, _ := os.Stat(m.active.Path())
	if info.Size() >= m.maxSize {
		m.Rotate()
	}

	return m.activeID, offset, nil
}

func (m *Manager) Path(id int) string {
	return filepath.Join(m.dir, fmt.Sprintf("wal-%06d.log", id))
}

func (m *Manager) ActiveID() int {
	return m.activeID
}
