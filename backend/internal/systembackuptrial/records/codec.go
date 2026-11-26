package records

import (
	"bytes"
	"encoding/json"
	"io"
)

// Encode must receive an encrypted staging writer. The payload contains hashes
// and credentials and must never be logged or stored as a plaintext JSON file.
func Encode(dst io.Writer, s Snapshot) error {
	if e := validateSnapshot(s); e != nil {
		return e
	}
	return json.NewEncoder(dst).Encode(s)
}
func Decode(src io.Reader, limits Limits) (Snapshot, error) {
	if !limits.valid() {
		return Snapshot{}, ErrCapacity
	}
	b, e := io.ReadAll(io.LimitReader(src, limits.MaxBytes+1))
	if e != nil {
		return Snapshot{}, ErrData
	}
	defer clear(b)
	if int64(len(b)) > limits.MaxBytes {
		return Snapshot{}, ErrCapacity
	}
	if strictJSON(b) != nil {
		return Snapshot{}, ErrData
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	var s Snapshot
	if d.Decode(&s) != nil {
		return Snapshot{}, ErrData
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return Snapshot{}, ErrData
	}
	var n int64
	for _, t := range s.Tables {
		n += int64(len(t.Rows))
	}
	if n > limits.MaxRows {
		return Snapshot{}, ErrCapacity
	}
	if e = validateSnapshot(s); e != nil {
		return Snapshot{}, e
	}
	return s, nil
}

// encoding/json otherwise accepts duplicate object keys. A signed/encrypted
// payload still needs one unambiguous interpretation by every owner adapter.
func strictJSON(b []byte) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 64 {
			return ErrData
		}
		token, e := d.Token()
		if e != nil {
			return ErrData
		}
		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					key, e := d.Token()
					if e != nil {
						return ErrData
					}
					name, ok := key.(string)
					if !ok || seen[name] {
						return ErrData
					}
					seen[name] = true
					if value(depth+1) != nil {
						return ErrData
					}
				}
				end, e := d.Token()
				if e != nil || end != json.Delim('}') {
					return ErrData
				}
			case '[':
				for d.More() {
					if value(depth+1) != nil {
						return ErrData
					}
				}
				end, e := d.Token()
				if e != nil || end != json.Delim(']') {
					return ErrData
				}
			default:
				return ErrData
			}
		}
		return nil
	}
	if value(0) != nil {
		return ErrData
	}
	if _, e := d.Token(); e != io.EOF {
		return ErrData
	}
	return nil
}
