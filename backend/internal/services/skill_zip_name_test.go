package services

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestDecodeZipEntryNameGB18030NonUTF8(t *testing.T) {
	name := mustEncodeGB18030(t, "公司上下游产业链分析/SKILL.md")
	entry := &zip.File{FileHeader: zip.FileHeader{Name: string(name), NonUTF8: true}}
	got := decodeZipEntryName(entry)
	if got != "公司上下游产业链分析/SKILL.md" {
		t.Fatalf("decodeZipEntryName() = %q, want Chinese UTF-8 path", got)
	}
	if !utf8.ValidString(got) {
		t.Fatal("decoded name must be valid UTF-8")
	}
}

func TestDecodeZipEntryNameMojibakeNonUTF8(t *testing.T) {
	// GBK bytes for 「撰写」 are valid UTF-8 mojibake; NonUTF8 must still force GB18030.
	name := mustEncodeGB18030(t, "ppt撰写/SKILL.md")
	if !utf8.Valid(name) {
		t.Fatal("fixture must be valid UTF-8 as raw bytes (mojibake case)")
	}
	entry := &zip.File{FileHeader: zip.FileHeader{Name: string(name), NonUTF8: true}}
	got := decodeZipEntryName(entry)
	if got != "ppt撰写/SKILL.md" {
		t.Fatalf("decodeZipEntryName() = %q (%q), want ppt撰写/SKILL.md", got, []byte(got))
	}
}

func TestDecodeZipEntryNameKeepsUTF8FlaggedPaths(t *testing.T) {
	entry := &zip.File{FileHeader: zip.FileHeader{Name: "邮件管家及任务管理助手/SKILL.md", NonUTF8: false}}
	got := decodeZipEntryName(entry)
	if got != "邮件管家及任务管理助手/SKILL.md" {
		t.Fatalf("decodeZipEntryName() = %q, want original UTF-8 path", got)
	}
}

func TestExtractSkillDirectoriesDecodesGBKRoot(t *testing.T) {
	cases := []struct {
		root string
	}{
		{root: "公司上下游产业链分析"},
		{root: "邮件管家及任务管理助手"},
	}
	for _, tc := range cases {
		t.Run(tc.root, func(t *testing.T) {
			archive := buildGBKTestZip(t, map[string][]byte{
				tc.root + "/SKILL.md": []byte("---\nname: " + tc.root + "\ndescription: test\n---\n# Skill\n"),
			})
			dirs, err := extractSkillDirectories(tc.root+".zip", archive)
			if err != nil {
				t.Fatalf("extractSkillDirectories() error = %v", err)
			}
			if len(dirs) != 1 {
				t.Fatalf("dirs = %d, want 1", len(dirs))
			}
			if dirs[0].Name != tc.root {
				t.Fatalf("Name = %q, want %q", dirs[0].Name, tc.root)
			}
			if _, ok := dirs[0].Files["SKILL.md"]; !ok {
				t.Fatalf("SKILL.md missing: %#v", dirs[0].Files)
			}
		})
	}
}

func TestFormatSkillScannerStatusErrorIncludesBody(t *testing.T) {
	got := formatSkillScannerStatusError(400, strings.NewReader("  file too large \n"))
	if got != "skill scanner returned status 400: file too large" {
		t.Fatalf("got %q", got)
	}
	got = formatSkillScannerStatusError(400, strings.NewReader(""))
	if got != "skill scanner returned status 400" {
		t.Fatalf("got %q", got)
	}
}

func TestScanArchiveIncludesErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"detail":"payload rejected"}`)
	}))
	defer server.Close()

	client := &httpSkillScannerClient{baseURL: server.URL, client: server.Client()}
	_, _, _, err := client.ScanArchive(context.Background(), "demo.zip", []byte("PK"), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "status 400") || !strings.Contains(err.Error(), "payload rejected") {
		t.Fatalf("error = %v", err)
	}
}

func mustEncodeGB18030(t *testing.T, value string) []byte {
	t.Helper()
	encoded, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(value))
	if err != nil {
		t.Fatalf("GB18030 encode %q: %v", value, err)
	}
	return encoded
}

func buildGBKTestZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		encoded := mustEncodeGB18030(t, name)
		header := &zip.FileHeader{
			Name:     string(encoded),
			Method:   zip.Deflate,
			NonUTF8:  true,
		}
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("CreateHeader(%q): %v", name, err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatalf("Write(%q): %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	return buffer.Bytes()
}
