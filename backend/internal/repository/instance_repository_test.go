package repository

import "testing"

func TestDecimalStringToIntAcceptsWholeDecimalStrings(t *testing.T) {
	value, err := decimalStringToInt("2.00")
	if err != nil {
		t.Fatalf("decimalStringToInt returned error: %v", err)
	}
	if value != 2 {
		t.Fatalf("expected 2, got %d", value)
	}
}
