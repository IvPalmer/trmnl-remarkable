package today

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type cachedSection struct {
	Raw       json.RawMessage `json:"raw"`
	FetchedAt time.Time       `json:"fetched_at"`
}

type cacheFile struct {
	Version  int                      `json:"version"`
	Sections map[string]cachedSection `json:"sections"`
}

// loadCache reads each section's last good data. A missing, unreadable or
// unknown-version cache is an empty one.
func loadCache(path string) map[string]cachedSection {
	b, err := os.ReadFile(path)
	if err != nil {
		return map[string]cachedSection{}
	}
	var f cacheFile
	if json.Unmarshal(b, &f) != nil || f.Version != 1 || f.Sections == nil {
		return map[string]cachedSection{}
	}
	return f.Sections
}

// saveCache replaces the cache atomically and owner-only: it holds personal
// items and mail snippets.
func saveCache(path string, sections map[string]cachedSection) error {
	b, err := json.Marshal(cacheFile{Version: 1, Sections: sections})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".today-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
