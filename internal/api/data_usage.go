package api

import (
	"io/fs"
	"path/filepath"
	"sync"
	"time"
)

const dataUsageMaxAge = time.Minute

type dataUsage struct {
	bytes     int64
	files     int
	diskFree  uint64
	diskTotal uint64
}

type measuredDataUsage struct {
	mutex      sync.Mutex
	measuredAt time.Time
	measured   bool
	usage      dataUsage
}

func measureDataUsage(dir string) dataUsage {
	var usage dataUsage
	filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if info, err := entry.Info(); err == nil {
			usage.bytes += info.Size()
			usage.files++
		}
		return nil
	})
	usage.diskFree, usage.diskTotal = diskSpace(dir)
	return usage
}

func (m *measuredDataUsage) current(dir string, now time.Time) dataUsage {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if dir == "" {
		return dataUsage{}
	}
	if age := now.Sub(m.measuredAt); !m.measured || age >= dataUsageMaxAge || age < 0 {
		m.usage, m.measuredAt, m.measured = measureDataUsage(dir), now, true
	}
	return m.usage
}
