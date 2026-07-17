package services

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"clawreef/internal/models"
)

func TestHashDirectoryPreservesSingleTopLevelSubdirectory(t *testing.T) {
	files := map[string][]byte{
		"src/main.py": []byte("print('hello')\n"),
	}

	got := hashDirectory(files)
	want := referenceSkillContentMD5(map[string][]byte{
		"src/main.py": []byte("print('hello')\n"),
	})
	if got != want {
		t.Fatalf("hashDirectory() = %s, want %s", got, want)
	}

	flattened := referenceSkillContentMD5(map[string][]byte{
		"main.py": []byte("print('hello')\n"),
	})
	if got == flattened {
		t.Fatalf("hashDirectory stripped the skill's internal src/ directory")
	}
}

func TestExtractSkillDirectoriesStripsArchiveRootOnlyOnce(t *testing.T) {
	archive := buildTestZip(t, map[string][]byte{
		"weather/SKILL.md":    []byte("# Weather\n"),
		"weather/src/main.py": []byte("print('weather')\n"),
	})

	dirs, err := extractSkillDirectories("weather.zip", archive)
	if err != nil {
		t.Fatalf("extractSkillDirectories() error = %v", err)
	}
	if len(dirs) != 1 {
		t.Fatalf("extractSkillDirectories() returned %d dirs, want 1", len(dirs))
	}
	if _, ok := dirs[0].Files["src/main.py"]; !ok {
		t.Fatalf("expected skill files to preserve src/main.py after stripping archive root once: %#v", dirs[0].Files)
	}

	got := hashDirectory(dirs[0].Files)
	want := referenceSkillContentMD5(map[string][]byte{
		"SKILL.md":    []byte("# Weather\n"),
		"src/main.py": []byte("print('weather')\n"),
	})
	if got != want {
		t.Fatalf("hashDirectory(extracted files) = %s, want %s", got, want)
	}
}

func TestExtractSkillDirectoriesAcceptsRootSkillPackage(t *testing.T) {
	archive := buildTestZip(t, map[string][]byte{
		".env":                []byte("TOKEN=example\n"),
		"SKILL.md":            []byte("# Weather\n"),
		"__MACOSX/._SKILL.md": []byte("metadata\n"),
		"scripts/run.sh":      []byte("python src/main.py\n"),
		"src/main.py":         []byte("print('weather')\n"),
	})

	dirs, err := extractSkillDirectories("weather.zip", archive)
	if err != nil {
		t.Fatalf("extractSkillDirectories() error = %v", err)
	}
	if len(dirs) != 1 {
		t.Fatalf("extractSkillDirectories() returned %d dirs, want 1", len(dirs))
	}
	if dirs[0].Name != "weather" {
		t.Fatalf("root skill package name = %q, want weather", dirs[0].Name)
	}
	if _, ok := dirs[0].Files["SKILL.md"]; !ok {
		t.Fatalf("expected root SKILL.md to be preserved: %#v", dirs[0].Files)
	}
	if _, ok := dirs[0].Files["scripts/run.sh"]; !ok {
		t.Fatalf("expected root scripts/run.sh to be preserved: %#v", dirs[0].Files)
	}
	if _, ok := dirs[0].Files[".env"]; !ok {
		t.Fatalf("expected root .env to be preserved for scanning: %#v", dirs[0].Files)
	}
	if _, ok := dirs[0].Files["__MACOSX/._SKILL.md"]; ok {
		t.Fatalf("expected archive metadata to be ignored: %#v", dirs[0].Files)
	}
}

func TestExtractSkillDirectoriesImportsMultipleManifestDirs(t *testing.T) {
	archive := buildTestZip(t, map[string][]byte{
		"alpha/SKILL.md": []byte("# Alpha\n"),
		"beta/SKILL.md":  []byte("# Beta\n"),
	})

	dirs, err := extractSkillDirectories("skills.zip", archive)
	if err != nil {
		t.Fatalf("extractSkillDirectories() error = %v", err)
	}
	if len(dirs) != 2 {
		t.Fatalf("extractSkillDirectories() returned %d dirs, want 2", len(dirs))
	}
	if dirs[0].Name != "alpha" || dirs[1].Name != "beta" {
		t.Fatalf("skill directory names = %q, %q; want alpha, beta", dirs[0].Name, dirs[1].Name)
	}
}

func TestExtractSkillDirectoriesRejectsTopLevelDirWithoutManifest(t *testing.T) {
	archive := buildTestZip(t, map[string][]byte{
		"weather/src/main.py": []byte("print('weather')\n"),
	})

	_, err := extractSkillDirectories("weather.zip", archive)
	if err == nil {
		t.Fatal("extractSkillDirectories() error = nil, want SKILL.md error")
	}
	if !strings.Contains(err.Error(), "SKILL.md") {
		t.Fatalf("extractSkillDirectories() error = %v, want SKILL.md error", err)
	}
}

func TestExtractSkillDirectoriesRejectsLooseFileWithoutRootManifest(t *testing.T) {
	archive := buildTestZip(t, map[string][]byte{
		"README.md":        []byte("not a skill manifest\n"),
		"weather/SKILL.md": []byte("# Weather\n"),
	})

	_, err := extractSkillDirectories("weather.zip", archive)
	if err == nil {
		t.Fatal("extractSkillDirectories() error = nil, want loose file error")
	}
	if !strings.Contains(err.Error(), "loose file README.md") {
		t.Fatalf("extractSkillDirectories() error = %v, want loose README.md error", err)
	}
}

func TestFlattenSingleTopLevelDirForArchiveRoot(t *testing.T) {
	files := map[string][]byte{
		"weather/src/main.py": []byte("print('weather')\n"),
	}

	got := hashDirectory(flattenSingleTopLevelDir(files))
	want := referenceSkillContentMD5(map[string][]byte{
		"src/main.py": []byte("print('weather')\n"),
	})
	if got != want {
		t.Fatalf("hashDirectory(flattenSingleTopLevelDir(files)) = %s, want %s", got, want)
	}
}

func TestMaterializeLiteInstanceSkillWritesOpenClawWorkspaceSkill(t *testing.T) {
	archive := buildTestZip(t, map[string][]byte{
		"paper-ranker/SKILL.md":      []byte("# Paper Ranker\n"),
		"paper-ranker/src/rank.py":   []byte("print('rank')\n"),
		"paper-ranker/.ignored-file": []byte("local secret\n"),
	})
	workspacePath := filepath.Join(t.TempDir(), "openclaw", "user-45", "instance-77")
	if err := os.MkdirAll(workspacePath, 0750); err != nil {
		t.Fatalf("MkdirAll(workspacePath): %v", err)
	}

	instanceRepo := newV2LifecycleInstanceRepo()
	instanceRepo.byID[77] = &models.Instance{
		ID:            0,
		UserID:        45,
		Type:          RuntimeTypeOpenClaw,
		RuntimeType:   RuntimeBackendGateway,
		InstanceMode:  InstanceModeLite,
		WorkspacePath: &workspacePath,
	}
	service := &skillService{
		instanceRepo: instanceRepo,
		storage:      fakeObjectStorage{"skills/paper-ranker.zip": archive},
	}

	err := service.materializeLiteInstanceSkill(context.Background(), 77, &models.Skill{
		SkillKey: "paper-ranker",
	}, &models.SkillBlob{
		ObjectKey: "skills/paper-ranker.zip",
		FileName:  "paper-ranker.zip",
	})
	if err != nil {
		t.Fatalf("materializeLiteInstanceSkill() error = %v", err)
	}

	target := filepath.Join(workspacePath, "home", ".openclaw", "workspace", "skills", "paper-ranker")
	assertFileEquals(t, filepath.Join(target, "SKILL.md"), "# Paper Ranker\n")
	assertFileEquals(t, filepath.Join(target, "src", "rank.py"), "print('rank')\n")
	if _, err := os.Stat(filepath.Join(target, ".ignored-file")); !os.IsNotExist(err) {
		t.Fatalf("hidden archive entry was materialized, stat err = %v", err)
	}
}

func TestMaterializeLiteInstanceSkillWritesHermesHomeSkill(t *testing.T) {
	archive := buildTestZip(t, map[string][]byte{
		"paper-ranker/SKILL.md": []byte("# Paper Ranker\n"),
	})
	workspacePath := filepath.Join(t.TempDir(), "hermes", "user-45", "instance-90")
	if err := os.MkdirAll(workspacePath, 0750); err != nil {
		t.Fatalf("MkdirAll(workspacePath): %v", err)
	}

	instanceRepo := newV2LifecycleInstanceRepo()
	instanceRepo.byID[90] = &models.Instance{
		ID:            90,
		UserID:        45,
		Type:          RuntimeTypeHermes,
		RuntimeType:   RuntimeBackendGateway,
		InstanceMode:  InstanceModeLite,
		WorkspacePath: &workspacePath,
	}
	service := &skillService{
		instanceRepo: instanceRepo,
		storage:      fakeObjectStorage{"skills/paper-ranker.zip": archive},
	}

	err := service.materializeLiteInstanceSkill(context.Background(), 90, &models.Skill{
		SkillKey: "paper-ranker",
	}, &models.SkillBlob{
		ObjectKey: "skills/paper-ranker.zip",
		FileName:  "paper-ranker.zip",
	})
	if err != nil {
		t.Fatalf("materializeLiteInstanceSkill() error = %v", err)
	}

	target := filepath.Join(workspacePath, "home", ".hermes", "skills", "paper-ranker")
	assertFileEquals(t, filepath.Join(target, "SKILL.md"), "# Paper Ranker\n")
}
func TestChownRuntimePathToleratesNonRootPermissionDenied(t *testing.T) {
	target := filepath.Join(t.TempDir(), "skill-file")
	if err := os.WriteFile(target, []byte("skill"), 0644); err != nil {
		t.Fatalf("WriteFile(target): %v", err)
	}

	oldChown := chownRuntimePathOwner
	oldEffectiveUID := currentEffectiveUID
	t.Cleanup(func() {
		chownRuntimePathOwner = oldChown
		currentEffectiveUID = oldEffectiveUID
	})
	chownRuntimePathOwner = func(string, int, int) error {
		return os.ErrPermission
	}
	currentEffectiveUID = func() int {
		return 1000
	}

	if err := chownRuntimePath(target, RuntimeLinuxID(77), RuntimeLinuxID(77), 0600); err != nil {
		t.Fatalf("chownRuntimePath() error = %v", err)
	}
	if os.PathSeparator == '/' {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatalf("Stat(target): %v", err)
		}
		if got := info.Mode().Perm(); got != 0600 {
			t.Fatalf("target mode = %v, want 0600", got)
		}
	}
}

func TestChownRuntimePathReportsRootPermissionDenied(t *testing.T) {
	target := filepath.Join(t.TempDir(), "skill-file")
	if err := os.WriteFile(target, []byte("skill"), 0644); err != nil {
		t.Fatalf("WriteFile(target): %v", err)
	}

	oldChown := chownRuntimePathOwner
	oldEffectiveUID := currentEffectiveUID
	t.Cleanup(func() {
		chownRuntimePathOwner = oldChown
		currentEffectiveUID = oldEffectiveUID
	})
	chownRuntimePathOwner = func(string, int, int) error {
		return os.ErrPermission
	}
	currentEffectiveUID = func() int {
		return 0
	}

	err := chownRuntimePath(target, RuntimeLinuxID(90), RuntimeLinuxID(90), 0600)
	if err == nil || !strings.Contains(err.Error(), "failed to set lite runtime owner") {
		t.Fatalf("chownRuntimePath() error = %v, want owner error", err)
	}
}
func TestWriteSkillDirectoryAtomicallyNestedCategoryPath(t *testing.T) {
	targetRoot := t.TempDir()
	err := writeSkillDirectoryAtomically(targetRoot, "productivity/my-skill", map[string][]byte{
		"SKILL.md": []byte("# Nested Skill\n"),
	})
	if err != nil {
		t.Fatalf("writeSkillDirectoryAtomically() error = %v", err)
	}
	target := filepath.Join(targetRoot, "productivity", "my-skill", "SKILL.md")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected nested skill directory, stat err = %v", err)
	}
}
func TestWriteSkillDirectoryAtomicallyUsesNestedTempRoot(t *testing.T) {
	targetRoot := t.TempDir()
	err := writeSkillDirectoryAtomically(targetRoot, "marker-pdf-ingest", map[string][]byte{
		"SKILL.md":              []byte("# Marker PDF Ingest\n"),
		"scripts/parse_pdf.py":  []byte("print('parse')\n"),
		"scripts/helpers/io.py": []byte("print('io')\n"),
	})
	if err != nil {
		t.Fatalf("writeSkillDirectoryAtomically() error = %v", err)
	}

	target := filepath.Join(targetRoot, "marker-pdf-ingest")
	assertFileEquals(t, filepath.Join(target, "scripts", "parse_pdf.py"), "print('parse')\n")
	if _, err := os.Stat(filepath.Join(targetRoot, ".tmp-skill-marker-pdf-ingest")); !os.IsNotExist(err) {
		t.Fatalf("temporary skill directory leaked at root, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetRoot, ".tmp")); err != nil {
		t.Fatalf("expected nested temporary root to remain available, stat err = %v", err)
	}
}
func TestRemoveLiteInstanceSkillDeletesOpenClawWorkspaceSkill(t *testing.T) {
	workspacePath := filepath.Join(t.TempDir(), "openclaw", "user-45", "instance-77")
	target := filepath.Join(workspacePath, "home", ".openclaw", "workspace", "skills", "paper-ranker")
	if err := os.MkdirAll(target, 0750); err != nil {
		t.Fatalf("MkdirAll(target): %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("# Paper Ranker\n"), 0640); err != nil {
		t.Fatalf("WriteFile(SKILL.md): %v", err)
	}

	instanceRepo := newV2LifecycleInstanceRepo()
	instanceRepo.byID[77] = &models.Instance{
		ID:            77,
		UserID:        45,
		Type:          RuntimeTypeOpenClaw,
		RuntimeType:   RuntimeBackendGateway,
		InstanceMode:  InstanceModeLite,
		WorkspacePath: &workspacePath,
	}
	installPath := "home/.openclaw/workspace/skills/paper-ranker"
	service := &skillService{instanceRepo: instanceRepo}

	if err := service.removeLiteInstanceSkillDirectory(77, &models.InstanceSkill{SkillID: 12, InstallPath: &installPath}); err != nil {
		t.Fatalf("removeLiteInstanceSkillDirectory() error = %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected skill directory to be removed, stat err = %v", err)
	}
}
func TestRemoveLiteInstanceSkillDeletesHermesHomeSkill(t *testing.T) {
	workspacePath := filepath.Join(t.TempDir(), "hermes", "user-45", "instance-90")
	target := filepath.Join(workspacePath, "home", ".hermes", "skills", "paper-ranker")
	if err := os.MkdirAll(target, 0750); err != nil {
		t.Fatalf("MkdirAll(target): %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("# Paper Ranker\n"), 0640); err != nil {
		t.Fatalf("WriteFile(SKILL.md): %v", err)
	}

	instanceRepo := newV2LifecycleInstanceRepo()
	instanceRepo.byID[90] = &models.Instance{
		ID:            90,
		UserID:        45,
		Type:          RuntimeTypeHermes,
		RuntimeType:   RuntimeBackendGateway,
		InstanceMode:  InstanceModeLite,
		WorkspacePath: &workspacePath,
	}
	installPath := "home/.hermes/skills/paper-ranker"
	service := &skillService{instanceRepo: instanceRepo}

	if err := service.removeLiteInstanceSkillDirectory(90, &models.InstanceSkill{SkillID: 12, InstallPath: &installPath}); err != nil {
		t.Fatalf("removeLiteInstanceSkillDirectory() error = %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected skill directory to be removed, stat err = %v", err)
	}
}
func TestLiteRuntimePersistentAncestorsIncludeOpenClawHome(t *testing.T) {
	workspacePath := filepath.Join(t.TempDir(), "openclaw", "user-1", "instance-89")
	persistentRoot := filepath.Join(workspacePath, "home", ".openclaw")

	got := liteRuntimePersistentAncestors(workspacePath, persistentRoot)
	want := []string{
		workspacePath,
		filepath.Join(workspacePath, "home"),
		filepath.Join(workspacePath, "home", ".openclaw"),
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("liteRuntimePersistentAncestors() = %#v, want %#v", got, want)
	}
}

type fakeObjectStorage map[string][]byte

func (f fakeObjectStorage) PutObject(context.Context, string, []byte, string) error {
	return nil
}

func (f fakeObjectStorage) GetObject(_ context.Context, objectKey string) ([]byte, error) {
	return f[objectKey], nil
}

func assertFileEquals(t *testing.T, filePath string, want string) {
	t.Helper()

	body, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", filePath, err)
	}
	if got := string(body); got != want {
		t.Fatalf("ReadFile(%q) = %q, want %q", filePath, got, want)
	}
}

func buildTestZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		entry, err := writer.Create(key)
		if err != nil {
			t.Fatalf("Create(%q): %v", key, err)
		}
		if _, err := entry.Write(files[key]); err != nil {
			t.Fatalf("Write(%q): %v", key, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	return buffer.Bytes()
}

func referenceSkillContentMD5(files map[string][]byte) string {
	entryKinds := map[string]string{}
	fileMap := map[string][]byte{}
	for key, body := range files {
		clean := path.Clean(key)
		if clean == "." || clean == "" {
			continue
		}
		fileMap[clean] = body
		entryKinds[clean] = "file"
		parts := splitTestPath(clean)
		for i := 1; i < len(parts); i++ {
			entryKinds[path.Join(parts[:i]...)] = "dir"
		}
	}

	keys := make([]string, 0, len(entryKinds))
	for key := range entryKinds {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	digest := md5.New()
	for _, key := range keys {
		_, _ = digest.Write([]byte(key))
		_, _ = digest.Write([]byte("\n"))
		if entryKinds[key] == "dir" {
			_, _ = digest.Write([]byte("dir\n"))
			continue
		}
		_, _ = digest.Write([]byte("file\n"))
		_, _ = digest.Write(fileMap[key])
		_, _ = digest.Write([]byte("\n"))
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func splitTestPath(value string) []string {
	result := []string{}
	for _, part := range bytes.Split([]byte(value), []byte("/")) {
		if len(part) > 0 {
			result = append(result, string(part))
		}
	}
	return result
}
