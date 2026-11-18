package systembackup

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sort"
	"strconv"
)

// Redis dictionaries use a per-process hash seed. RDB hash/set entries may
// therefore change order after RESTORE even when every binary value survives.
// Preserve the original DUMP; canonicalize only unordered dictionary entries
// for comparison. Packed representations retain their original byte comparison.
func redisDumpsEqual(kind string, expected, actual []byte) (bool, error) {
	if bytes.Equal(expected, actual) {
		return true, nil
	}
	if kind != "hash" && kind != "set" {
		return false, nil
	}
	a, e := unorderedRedisDumpDigest(kind, expected)
	if e != nil {
		return false, e
	}
	b, e := unorderedRedisDumpDigest(kind, actual)
	if e != nil {
		return false, e
	}
	return a == b, nil
}

type redisDumpCursor struct {
	data      []byte
	pos       int
	remaining uint64
}

var errRedisDump = errors.New("unsupported or malformed Redis dictionary dump")

func (p *redisDumpCursor) take(n uint64) ([]byte, error) {
	if n > uint64(len(p.data)-p.pos) {
		return nil, errRedisDump
	}
	out := p.data[p.pos : p.pos+int(n)]
	p.pos += int(n)
	return out, nil
}
func (p *redisDumpCursor) length() (uint64, int, error) {
	b, e := p.take(1)
	if e != nil {
		return 0, -1, e
	}
	v := b[0]
	switch v >> 6 {
	case 0:
		return uint64(v & 63), -1, nil
	case 1:
		b, e = p.take(1)
		if e != nil {
			return 0, -1, e
		}
		return uint64(v&63)<<8 | uint64(b[0]), -1, nil
	case 2:
		if v == 0x80 {
			b, e = p.take(4)
			if e != nil {
				return 0, -1, e
			}
			return uint64(binary.BigEndian.Uint32(b)), -1, nil
		}
		if v == 0x81 {
			b, e = p.take(8)
			if e != nil {
				return 0, -1, e
			}
			return binary.BigEndian.Uint64(b), -1, nil
		}
		return 0, -1, errRedisDump
	default:
		return 0, int(v & 63), nil
	}
}
func (p *redisDumpCursor) str() ([]byte, error) {
	n, encoding, e := p.length()
	if e != nil {
		return nil, e
	}
	var out []byte
	switch encoding {
	case -1:
		if n > p.remaining {
			return nil, errRedisDump
		}
		out, e = p.take(n)
	case 0, 1, 2:
		size := uint64(1) << encoding
		var raw []byte
		raw, e = p.take(size)
		if e != nil {
			return nil, e
		}
		var value int64
		switch encoding {
		case 0:
			value = int64(int8(raw[0]))
		case 1:
			value = int64(int16(binary.LittleEndian.Uint16(raw)))
		case 2:
			value = int64(int32(binary.LittleEndian.Uint32(raw)))
		}
		out = []byte(strconv.FormatInt(value, 10))
	case 3:
		compressed, ce, e := p.length()
		if e != nil || ce != -1 {
			return nil, errRedisDump
		}
		expanded, xe, e := p.length()
		if e != nil || xe != -1 || expanded > p.remaining {
			return nil, errRedisDump
		}
		raw, e := p.take(compressed)
		if e != nil {
			return nil, e
		}
		out, e = decodeRedisLZF(raw, expanded)
		if e != nil {
			return nil, e
		}
	default:
		return nil, errRedisDump
	}
	if e != nil || uint64(len(out)) > p.remaining {
		return nil, errRedisDump
	}
	p.remaining -= uint64(len(out))
	return out, nil
}
func decodeRedisLZF(input []byte, size uint64) ([]byte, error) {
	if size > 64<<20 {
		return nil, errRedisDump
	}
	out := make([]byte, int(size))
	ip, op := 0, 0
	for ip < len(input) {
		control := int(input[ip])
		ip++
		if control < 32 {
			n := control + 1
			if n > len(input)-ip || n > len(out)-op {
				return nil, errRedisDump
			}
			copy(out[op:op+n], input[ip:ip+n])
			ip += n
			op += n
			continue
		}
		n := control >> 5
		offset := (control & 31) << 8
		if n == 7 {
			if ip >= len(input) {
				return nil, errRedisDump
			}
			n += int(input[ip])
			ip++
		}
		if ip >= len(input) {
			return nil, errRedisDump
		}
		offset += int(input[ip]) + 1
		ip++
		n += 2
		ref := op - offset
		if ref < 0 || n > len(out)-op {
			return nil, errRedisDump
		}
		for j := 0; j < n; j++ {
			out[op] = out[ref]
			op++
			ref++
		}
	}
	if op != len(out) {
		return nil, errRedisDump
	}
	return out, nil
}
func unorderedRedisDumpDigest(kind string, dump []byte) ([32]byte, error) {
	var zero [32]byte
	if len(dump) < 11 || len(dump) > 64<<20 {
		return zero, errRedisDump
	}
	if (kind == "hash" && dump[0] != 4) || (kind == "set" && dump[0] != 2) {
		return zero, errRedisDump
	}
	p := redisDumpCursor{data: dump[1 : len(dump)-10], remaining: 64 << 20}
	count, encoded, e := p.length()
	if e != nil || encoded != -1 || count > 1000000 || count > uint64(len(p.data)) {
		return zero, errRedisDump
	}
	type entry struct{ key, value []byte }
	entries := make([]entry, 0, int(count))
	for i := uint64(0); i < count; i++ {
		key, e := p.str()
		if e != nil {
			return zero, e
		}
		var value []byte
		if kind == "hash" {
			value, e = p.str()
			if e != nil {
				return zero, e
			}
		}
		entries = append(entries, entry{key, value})
	}
	if p.pos != len(p.data) {
		return zero, errRedisDump
	}
	sort.Slice(entries, func(i, j int) bool { return bytes.Compare(entries[i].key, entries[j].key) < 0 })
	h := sha256.New()
	h.Write([]byte("cm-redis-dictionary.v1\x00" + kind + "\x00"))
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], count)
	h.Write(length[:])
	for i, v := range entries {
		if i > 0 && bytes.Equal(v.key, entries[i-1].key) {
			return zero, errRedisDump
		}
		for _, part := range [][]byte{v.key, v.value} {
			binary.BigEndian.PutUint64(length[:], uint64(len(part)))
			h.Write(length[:])
			h.Write(part)
		}
	}
	copy(zero[:], h.Sum(nil))
	return zero, nil
}
