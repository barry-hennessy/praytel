package archmap

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Written is one map file whose contents changed.
type Written struct {
	Path    string
	Created bool // there was no file before
	Map     Map  // what was written
	Diff    Diff // the file's previous contents against its new ones
}

// Write writes each fragment to its file below dir (see FileName), leaving
// files whose contents are already right, and files for other entry
// points, untouched. It returns the files it changed, in entry order.
func Write(dir string, frags Fragments) ([]Written, error) {
	var out []Written
	for _, e := range sortedKeys(keysOf(frags), compareNodes) {
		frag := frags[e]
		data, err := Format(frag)
		if err != nil {
			return out, err
		}
		path := filepath.Join(dir, FileName(e))
		prev, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return out, err
		}
		if bytes.Equal(prev, data) {
			continue
		}
		old := Fragments{}
		if m, err := Parse(bytes.NewReader(prev)); err == nil && len(prev) > 0 {
			old[e] = m
		}
		if err := writeFile(path, data); err != nil {
			return out, err
		}
		out = append(out, Written{Path: path, Created: prev == nil, Map: frag, Diff: Compare(old, Fragments{e: frag})})
	}
	return out, nil
}

// writeFile replaces path's contents in one step, so a reader never sees
// half a map.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".praytel-map-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func keysOf(f Fragments) map[Node]bool {
	out := make(map[Node]bool, len(f))
	for k := range f {
		out[k] = true
	}
	return out
}
