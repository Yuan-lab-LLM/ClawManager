package services

import (
	"errors"
	"testing"

	"clawreef/internal/utils"
)

func TestHashDirectoryGoldenWeatherFixture(t *testing.T) {
	files := map[string][]byte{
		"src/main.py": []byte("print('weather')\n"),
	}
	got := hashDirectory(files)
	want := referenceSkillContentMD5(files)
	if got != want {
		t.Fatalf("hashDirectory() = %s, want %s", got, want)
	}
}

func TestHubErrorMD5MismatchCode(t *testing.T) {
	err := utils.NewHubError(
		"skill_package_md5_mismatch",
		"skill package md5 mismatch: expected abc got def",
		map[string]string{"expected": "abc", "computed": "def"},
	)
	var hubErr *utils.HubError
	if !errors.As(err, &hubErr) {
		t.Fatal("expected HubError")
	}
	if hubErr.Code != "skill_package_md5_mismatch" {
		t.Fatalf("expected skill_package_md5_mismatch, got %q", hubErr.Code)
	}
}
