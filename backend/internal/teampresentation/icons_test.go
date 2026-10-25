package teampresentation

import "testing"

func TestResolveFunctionalIdentity(t *testing.T) {
	cases := []struct {
		name  string
		input Input
		want  string
	}{
		{"leader overrides decoration", Input{Leader: true, Override: "data"}, "leader"},
		{"profile", Input{ProfileKey: "agency.backend-architect", Role: "worker"}, "backend"},
		{"custom profile is not a profession", Input{ProfileKey: "custom.team-template.9.worker", Role: "worker", Tags: []string{"news-collection"}}, "news"},
		{"raw Chinese role", Input{Role: "数据分析师"}, "data"},
		{"named legacy member", Input{Name: "论文精读专员", Role: "worker"}, "literature"},
		{"manual selection", Input{Override: "editor", Role: "data-analyst"}, "editor"},
		{"ambiguous capabilities", Input{Tags: []string{"data-analysis", "frontend"}}, "general"},
		{"no substring guessing", Input{Name: "Not a leader", Role: "worker"}, "general"},
		{"unknown role", Input{Role: "future-profession"}, "general"},
		{"invalid decoration", Input{Override: "<svg onload=alert(1)>", Role: "developer"}, "developer"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Resolve(c.input); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestDecorationAllowlist(t *testing.T) {
	for _, key := range []string{"leader", "user", "system", "unknown", "https://example.test/a.svg"} {
		if ValidOverride(key) {
			t.Fatalf("worker override accepted identity %s", key)
		}
	}
	if !ValidOverride("") || !ValidOverride("data") {
		t.Fatal("auto or data not accepted")
	}
}
