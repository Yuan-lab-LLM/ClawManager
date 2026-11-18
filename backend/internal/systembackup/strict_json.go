package systembackup

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// DecodeStrictJSON is shared by offline bootstrap and API contract readers.
// It rejects duplicate/case-ambiguous keys as well as unknown typed fields.
func DecodeStrictJSON(raw []byte, out any) error { return strictDecode(raw, out) }

// Reject ambiguous objects before decoding typed fields. encoding/json normally
// accepts duplicate keys (and case-insensitive struct matches), which is unsafe
// for signed manifests and normalized record digests.
func uniqueJSONKeys(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 64 {
			return errors.New("JSON nesting limit exceeded")
		}
		token, e := d.Token()
		if e != nil {
			return e
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, e := d.Token()
				if e != nil {
					return e
				}
				name, ok := key.(string)
				if !ok {
					return errors.New("invalid JSON key")
				}
				name = strings.ToLower(name)
				if seen[name] {
					return errors.New("ambiguous JSON key")
				}
				seen[name] = true
				if e = walk(depth + 1); e != nil {
					return e
				}
			}
			token, e = d.Token()
			if e != nil || token != json.Delim('}') {
				return errors.New("invalid JSON object")
			}
		case '[':
			for d.More() {
				if e = walk(depth + 1); e != nil {
					return e
				}
			}
			token, e = d.Token()
			if e != nil || token != json.Delim(']') {
				return errors.New("invalid JSON array")
			}
		default:
			return errors.New("invalid JSON delimiter")
		}
		return nil
	}
	if e := walk(0); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return errors.New("trailing JSON content")
	}
	return nil
}
