package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAbsoluteDeadlineCannotBeExtendedByJobRecreation(t *testing.T) {
	now := time.Date(2026, 10, 24, 1, 0, 0, 0, time.UTC)
	frozen := now.Add(10 * time.Minute)
	for _, elapsed := range []time.Duration{0, 9 * time.Minute} {
		got, e := executionDeadline(now.Add(elapsed), time.Hour, frozen.Format(time.RFC3339Nano))
		if e != nil || !got.Equal(frozen) {
			t.Fatal("restart extended task deadline", got, e)
		}
	}
	if _, e := executionDeadline(frozen, time.Hour, frozen.Format(time.RFC3339Nano)); e == nil || e.Error() != "deadline_exceeded" {
		t.Fatal("expired job could start", e)
	}
	if _, e := executionDeadline(now, time.Hour, "invalid"); e == nil {
		t.Fatal("invalid absolute deadline accepted")
	}
	got, e := executionDeadline(now, time.Minute, frozen.Format(time.RFC3339Nano))
	if e != nil || !got.Equal(now.Add(time.Minute)) {
		t.Fatal("relative timeout extended", e)
	}
}

func TestBootstrapFilesAreBoundedAndDoNotExposeContents(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bootstrap.json")
	secret := []byte(`{"unknown":"secret-that-must-not-be-logged"}`)
	if e := os.WriteFile(p, secret, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := readBounded(p, 4); e == nil {
		t.Fatal("oversized bootstrap accepted")
	}
	if _, e := readBounded(dir, 16384); e == nil {
		t.Fatal("directory accepted")
	}
	var cfg bootstrap
	if e := decodeFile(p, &cfg); e == nil || e.Error() != "invalid bootstrap configuration" {
		t.Fatal("unexpected configuration failure", e)
	}
	if e := os.WriteFile(p, []byte(`{} {}`), 0600); e != nil {
		t.Fatal(e)
	}
	if e := decodeFile(p, &cfg); e == nil {
		t.Fatal("trailing configuration accepted")
	}
	if e := os.WriteFile(p, []byte(`{"secure":true,"SECURE":false}`), 0600); e != nil {
		t.Fatal(e)
	}
	if e := decodeFile(p, &cfg); e == nil {
		t.Fatal("ambiguous bootstrap configuration accepted")
	}
}
