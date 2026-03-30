package services

import "testing"

func TestBuildProviderEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		baseURL       string
		versionPrefix string
		resource      string
		want          string
		wantErr       bool
	}{
		{
			name:          "appends default version when base url has no version suffix",
			baseURL:       "https://api.deepseek.com",
			versionPrefix: "v1",
			resource:      "models",
			want:          "https://api.deepseek.com/v1/models",
		},
		{
			name:          "keeps matching version suffix",
			baseURL:       "https://api.openai.com/v1",
			versionPrefix: "v1",
			resource:      "models",
			want:          "https://api.openai.com/v1/models",
		},
		{
			name:          "respects nonstandard version suffix",
			baseURL:       "https://open.bigmodel.cn/api/paas/v4",
			versionPrefix: "v1",
			resource:      "models",
			want:          "https://open.bigmodel.cn/api/paas/v4/models",
		},
		{
			name:          "keeps beta version suffix",
			baseURL:       "https://generativelanguage.googleapis.com/v1beta",
			versionPrefix: "v1beta",
			resource:      "models",
			want:          "https://generativelanguage.googleapis.com/v1beta/models",
		},
		{
			name:          "rejects invalid base url",
			baseURL:       "not-a-url",
			versionPrefix: "v1",
			resource:      "models",
			wantErr:       true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := buildProviderEndpoint(tc.baseURL, tc.versionPrefix, tc.resource)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got none and endpoint %q", got)
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
