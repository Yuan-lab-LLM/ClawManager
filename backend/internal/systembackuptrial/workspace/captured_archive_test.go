package workspace

import (
	"archive/tar"
	"encoding/base64"
	"testing"
)

func TestCapturedArchiveValidatorKeepsExplicitLinuxLayoutExceptions(t *testing.T) {
	for _, layout := range []Layout{WorkBuddyWineV1, WorkBuddyInstanceWineV1} {
		policy, _ := NewCapturedArchiveValidator(layout)
		names := []string{".", ".local", ".local/share", WinePrefix, WinePrefix + "/drive_c", WinePrefix + "/dosdevices"}
		if layout == WorkBuddyInstanceWineV1 {
			if policy.Observe(&tar.Header{Name: "home", Typeflag: tar.TypeDir, Mode: 0700}) != nil {
				t.Fatal("home")
			}
		}
		for _, name := range names {
			if layout == WorkBuddyInstanceWineV1 && name == "." {
				continue
			}
			if policy.Observe(&tar.Header{Name: layout.winePath(name), Typeflag: tar.TypeDir, Mode: 02770, PAXRecords: map[string]string{wineDOSPAX: base64.StdEncoding.EncodeToString([]byte("0x20"))}}) != nil {
				t.Fatal("valid Linux setgid/DOS directory rejected", name)
			}
		}
		for _, file := range []string{"system.reg", "user.reg", "userdef.reg"} {
			if policy.Observe(&tar.Header{Name: layout.winePath(WinePrefix + "/" + file), Typeflag: tar.TypeReg, Mode: 0600}) != nil {
				t.Fatal("Wine registry")
			}
		}
		for name, target := range map[string]string{wineC: "../drive_c", wineZ: "/"} {
			if policy.Observe(&tar.Header{Name: layout.winePath(name), Typeflag: tar.TypeSymlink, Linkname: target, Mode: 0777}) != nil {
				t.Fatal("explicit Wine link rejected")
			}
		}
		if policy.Finish() != nil {
			t.Fatal("valid full Wine mapping rejected")
		}
	}
	policy, _ := NewCapturedArchiveValidator(DeepSeekHarnessV1)
	for _, name := range []string{".dsh", ".dsh/profiles", deepSeekModules} {
		if policy.Observe(&tar.Header{Name: name, Typeflag: tar.TypeDir, Mode: 0700}) != nil {
			t.Fatal("DeepSeek directory")
		}
	}
	if policy.Observe(&tar.Header{Name: deepSeekModules + "/react-dom", Typeflag: tar.TypeSymlink, Linkname: deepSeekImage + "/node_modules/@deepseek-ai/dsh-client-ui-trajectory/node_modules/react-dom", Mode: 0777}) != nil || policy.Finish() != nil {
		t.Fatal("explicit DeepSeek image link rejected")
	}
}

func TestCapturedArchiveValidatorRejectsUnknownTypesAndAliases(t *testing.T) {
	for _, header := range []*tar.Header{
		{Name: "escape", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd", Mode: 0777},
		{Name: "device", Typeflag: tar.TypeChar, Mode: 0600},
		{Name: "hard", Typeflag: tar.TypeLink, Linkname: "file", Mode: 0600},
		{Name: "file", Typeflag: tar.TypeReg, Mode: 04755},
		{Name: "file", Typeflag: tar.TypeReg, Mode: 0600, PAXRecords: map[string]string{"SCHILY.xattr.security.capability": "untrusted"}},
	} {
		policy, _ := NewCapturedArchiveValidator("")
		if policy.Observe(header) == nil {
			t.Fatal("unsafe archived header accepted", header.Name)
		}
	}
	for _, reverse := range []bool{false, true} {
		policy, _ := NewCapturedArchiveValidator("")
		a, b := &tar.Header{Name: "alias", Typeflag: tar.TypeSymlink, Linkname: "other", Mode: 0777}, &tar.Header{Name: "alias/file", Typeflag: tar.TypeReg, Mode: 0600}
		if reverse {
			a, b = b, a
		}
		if policy.Observe(a) != nil || policy.Observe(b) == nil {
			t.Fatal("payload below symlink accepted", reverse)
		}
	}
}
