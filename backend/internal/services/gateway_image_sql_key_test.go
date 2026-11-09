package services

import (
	"bytes"
	"fmt"
	"testing"
)

func TestImageSQLKeyBinaryValueAndLogRedaction(t *testing.T) {
	key := imageSQLKey{bytes: [32]byte{0, 255, 128, 42, 23}}
	raw, err := key.Value()
	if err != nil {
		t.Fatal(err)
	}
	binary, ok := raw.([]byte)
	if !ok || len(binary) != 32 || !bytes.Equal(binary, key.bytes[:]) {
		t.Fatal("SQL key is not raw binary")
	}
	for _, formatted := range []string{fmt.Sprintf("%v", key), fmt.Sprintf("%#v", key), fmt.Sprintf("%+v", key)} {
		if formatted != "[redacted image key]" {
			t.Fatal("key leaked into SQL diagnostics", formatted)
		}
	}
}
