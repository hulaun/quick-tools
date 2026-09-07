package macro

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Editing macros.json.
//
// The same arrangement as places.json, and for the same reason: every entry the
// palette is not touching is carried across as the raw bytes it was read as, so
// a "_comment", a hand-tuned "delay", or a field a later version adds all
// survive a rename or a delete somewhere else in the file. Only the entry
// actually being changed is decoded and written back.
//
// Indentation is normalised. That cannot be helped and is the whole cost.

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

	// A nil slice marshals as "null", which is not a macros file. The empty
	// array is.
	if out == nil {
		out = []json.RawMessage{}
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}

	// Unlike places.json, this file is written before anyone has necessarily
	// created the folder around it: the first thing a new user does in this tab
	// is record, and storage/ may not exist in a fresh checkout.
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.path, append(data, '\n'), 0o644)
}

// Add appends a macro and returns the index it was written at.
func (s *Store) Add(m Macro) (int, error) {
	if m.Steps == nil {
		// An explicit empty list, so a macro that has been created but not yet
		// recorded reads as one rather than as a missing field.
		m.Steps = []Step{}
	}
	added := -1
	err := s.rewrite(func(raws []json.RawMessage) ([]json.RawMessage, error) {
		entry, err := json.Marshal(m)
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
func (s *Store) SetName(index int, name string) error {
	if name == "" {
		return fmt.Errorf("a name cannot be empty")
	}
	return s.setField(index, "name", name)
}

// SetSteps replaces the entry's steps, which is what finishing a recording
// does. Everything else about the macro -- its name, group, delay and undo
// override -- is left exactly as it was, so re-recording a macro keeps the
// tuning that was done to it.
func (s *Store) SetSteps(index int, steps []Step) error {
	if steps == nil {
		steps = []Step{}
	}
	return s.setField(index, "steps", steps)
}

// setField is the one place an entry is decoded, changed and written back.
func (s *Store) setField(index int, key string, value any) error {
	return s.rewrite(func(raws []json.RawMessage) ([]json.RawMessage, error) {
		if index < 0 || index >= len(raws) {
			return nil, fmt.Errorf("no macro at index %d", index)
		}

		// UseNumber, so a number in a field this version does not know about is
		// written back exactly as it was read. Decoding into a plain any turns
		// every number into a float64, which silently rounds a large one -- the
		// same trap the JSON transforms have a test pinning.
		dec := json.NewDecoder(bytes.NewReader(raws[index]))
		dec.UseNumber()
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			return nil, err
		}
		if m == nil {
			m = map[string]any{}
		}
		m[key] = value

		entry, err := json.Marshal(m)
		if err != nil {
			return nil, err
		}
		raws[index] = entry
		return raws, nil
	})
}

// Delete removes the entry at index.
func (s *Store) Delete(index int) error {
	return s.rewrite(func(raws []json.RawMessage) ([]json.RawMessage, error) {
		if index < 0 || index >= len(raws) {
			return nil, fmt.Errorf("no macro at index %d", index)
		}
		return append(raws[:index], raws[index+1:]...), nil
	})
}
