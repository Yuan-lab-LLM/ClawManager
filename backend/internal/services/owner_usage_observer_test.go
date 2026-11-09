package services

import (
	"bytes"
	"io"
	"testing"
)

func TestOwnerObservationNeverChangesForwardedBytes(t *testing.T) {
	for _, size := range []int{0, 1, 256 << 10, (256 << 10) + 1, 2 << 20} {
		payload := bytes.Repeat([]byte{0xff}, size)
		capture := &boundedUsageFrame{}
		var forwarded bytes.Buffer
		if _, err := io.Copy(&forwarded, io.TeeReader(bytes.NewReader(payload), capture)); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(payload, forwarded.Bytes()) {
			t.Fatal("proxy payload changed")
		}
		if size <= 256<<10 {
			if capture.overflow || !bytes.Equal(capture.data, payload) {
				t.Fatal("bounded observation failed")
			}
		} else if !capture.overflow || len(capture.data) != 0 {
			t.Fatal("oversized payload retained")
		}
	}
}
