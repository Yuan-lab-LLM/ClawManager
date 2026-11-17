//go:build linux

package main

import (
	"os"
	"strconv"
	"testing"
)

func TestBackupFlockOwnsOpenDescriptionAndRefusesCompetitor(t *testing.T) {
	f, e := os.CreateTemp(t.TempDir(), "lock")
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	other, e := os.OpenFile(f.Name(), os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	if code := run([]string{"-n", "-x", strconv.Itoa(int(f.Fd()))}); code != 0 {
		t.Fatal("owner rejected", code)
	}
	if code := run([]string{"-n", "-x", strconv.Itoa(int(other.Fd()))}); code == 0 {
		t.Fatal("competitor acquired live lock")
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
	if code := run([]string{"-n", "-x", strconv.Itoa(int(other.Fd()))}); code != 0 {
		t.Fatal("released lock not reclaimable", code)
	}
	if code := run([]string{"-x", "-1"}); code == 0 {
		t.Fatal("unsafe descriptor/flags accepted")
	}
}
