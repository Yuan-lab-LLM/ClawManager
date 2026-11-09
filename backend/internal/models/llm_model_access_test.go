package models

import "testing"

func TestModelVisibility(t *testing.T) {
	for _, tc := range []struct {
		scope string
		ids   []int
		owner int
		want  bool
	}{
		{"all", nil, 1, true}, {"", nil, 1, true}, {"all", nil, 0, false},
		{"selected_users", []int{1, 2}, 1, true}, {"selected_users", []int{1, 2}, 3, false},
		{"selected_users", nil, 1, false}, {"unknown", []int{1}, 1, false},
	} {
		m := &LLMModel{VisibilityScope: tc.scope, AllowedUserIDs: tc.ids}
		if m.VisibleTo(tc.owner) != tc.want {
			t.Fatalf("%+v", tc)
		}
	}
}
