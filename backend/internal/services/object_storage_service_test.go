package services

import (
	"testing"

	"github.com/minio/minio-go/v7"
)

func TestBucketLookupType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		forcePathStyle bool
		want           minio.BucketLookupType
	}{
		{
			name:           "force path style",
			forcePathStyle: true,
			want:           minio.BucketLookupPath,
		},
		{
			name:           "allow automatic lookup",
			forcePathStyle: false,
			want:           minio.BucketLookupAuto,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := bucketLookupType(testCase.forcePathStyle); got != testCase.want {
				t.Fatalf("bucketLookupType(%t) = %v, want %v", testCase.forcePathStyle, got, testCase.want)
			}
		})
	}
}
