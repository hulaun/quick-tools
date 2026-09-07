package place

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// Editing places.json.
//
// Every operation rewrites the whole file, and every entry it is not touching
// is carried across as the raw bytes it was read as. That is what keeps a
// hand-written file hand-written: a "_comment" key, a field this version does
// not know about, or a value written in a form Go would marshal differently
// all survive a rename or a delete of some other entry. Only the entry actually
// being changed is decoded and written back.
//
// Indentation is normalised, because there is no way to preserve it. That is
// the one cost, and it is the same one gofmt imposes.

// readRaw returns the file as a list of untouched entries. A missing file is
// an empty list, not an error: the first place added creates it.
func readRaw(path string) ([]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}

	var raws []json.RawMessage
	if err := json.Unmarshal(data, &raws); err != nil {
		return nil, err
	}
	return raws, nil
}

// rewrite applies a change to the entry list and writes it back.
func (s *Store) rewrite(fn func([]json.RawMessage) ([]json.RawMessage, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	raws, err := readRaw(s.path)
	if err != nil {
		return err
	}
	out, err := fn(raws)
	if err != nil {
		return err
	}

	// An empty list marshals as "null" from a nil slice, which is not a places
	// file. The empty array is.
	if out == nil {
		out = []json.RawMessage{}
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(data, '\n'), 0o644)
}

// Add appends a place and returns the index it was written at.
func (s *Store) Add(pl Place) (int, error) {
	added := -1
	err := s.rewrite(func(raws []json.RawMessage) ([]json.RawMessage, error) {
		entry, err := json.Marshal(pl)
		if err != nil {
			return nil, err
		}
		added = len(raws)
		return append(raws, entry), nil
	})
	if err != nil {
		return -1, err
	}
	return added, nil
}

// SetName renames the entry at index.
//
// The entry is decoded into a map rather than into a Place so that fields this
// version does not know about are written back with it instead of being
// dropped on the way through.
func (s *Store) SetName(index int, name string) error {
	if name == "" {
		return fmt.Errorf("a name cannot be empty")
	}
	return s.rewrite(func(raws []json.RawMessage) ([]json.RawMessage, error) {
		if index < 0 || index >= len(raws) {
			return nil, fmt.Errorf("no place at index %d", index)
		}

		// UseNumber, so a number in an unknown field is written back exactly as it
		// was read. Decoding into a plain any turns every number into a float64,
		// which silently mangles a 64-bit value -- the same trap the JSON
		// transforms have a test pinning.
		dec := json.NewDecoder(bytes.NewReader(raws[index]))
		dec.UseNumber()
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			return nil, err
		}
		if m == nil {
			m = map[string]any{}
		}
		m["name"] = name

		entry, err := json.Marshal(m)
		if err != nil {
			return nil, err
		}
		raws[index] = entry
		return raws, nil
	})
}

// Delete removes the entry at index.
//
// It removes the line from places.json and nothing else -- the folder or file
// it pointed at is not touched. A place is a shortcut, and deleting a shortcut
// has never meant deleting what it points to.
func (s *Store) Delete(index int) error {
	return s.rewrite(func(raws []json.RawMessage) ([]json.RawMessage, error) {
		if index < 0 || index >= len(raws) {
			return nil, fmt.Errorf("no place at index %d", index)
		}
		return append(raws[:index], raws[index+1:]...), nil
	})
}
