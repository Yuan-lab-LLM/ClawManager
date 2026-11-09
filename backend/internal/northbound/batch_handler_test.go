package northbound

import "testing"

func TestNormalizeLifecycleBatch(t *testing.T) {
	ids, err := normalizeBatchRequest([]int{3, 1, 3, 2}, "restart")
	if err != nil || len(ids) != 3 || ids[0] != 1 || ids[2] != 3 {
		t.Fatalf("normalization: %v %v", ids, err)
	}
	for _, action := range []string{"delete", "", "stop"} {
		if _, err := normalizeBatchRequest([]int{1}, action); err == nil {
			t.Fatalf("accepted %q", action)
		}
	}
	for _, ids := range [][]int{nil, {-1}, make([]int, 501)} {
		if _, err := normalizeBatchRequest(ids, "reset"); err == nil {
			t.Fatal("accepted invalid selection")
		}
	}
}
