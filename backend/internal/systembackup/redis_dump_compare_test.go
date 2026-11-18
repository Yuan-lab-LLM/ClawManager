package systembackup

import "testing"

func TestRedisDictionaryDumpRejectsMalformedData(t *testing.T) {
	for _, payload := range [][]byte{
		{}, {4}, {4, 1, 1, 'k'},
		{4, 1, 0xc3, 1, 0x80, 0xff, 0xff, 0xff, 0xff, 0},
		{2, 2, 1, 'x', 1, 'x'}, // Duplicate set member.
		{2, 1, 1, 'x', 0},      // Trailing payload.
	} {
		dump := append(append([]byte{}, payload...), make([]byte, 10)...)
		kind := "hash"
		if len(payload) > 0 && payload[0] == 2 {
			kind = "set"
		}
		if _, err := unorderedRedisDumpDigest(kind, dump); err == nil {
			t.Fatalf("accepted malformed dictionary: %x", payload)
		}
	}
	for _, input := range [][]byte{{0}, {32, 0}, {224}, {0, 'x'}} {
		if _, err := decodeRedisLZF(input, 4); err == nil {
			t.Fatalf("accepted invalid compressed dictionary: %x", input)
		}
	}
}
