package services

import (
	"os"
	"path/filepath"
	"strings"

	"clawreef/internal/models"
)

const runtimeSkillDiscoveryMaxDepth = 2

type runtimeSkillDiscovery struct {
	RelativePath string
	SkillRoot    string
}

func runtimeSkillInstallRoot(instance *models.Instance) string {
	if instance == nil || instance.WorkspacePath == nil || strings.TrimSpace(*instance.WorkspacePath) == "" {
		return ""
	}
	workspacePath := filepath.Clean(strings.TrimSpace(*instance.WorkspacePath))
	if isLiteRuntimeInstance(instance) {
		if strings.EqualFold(strings.TrimSpace(instance.Type), RuntimeTypeHermes) {
			return filepath.Join(workspacePath, "home", ".hermes", "skills")
		}
		return filepath.Join(workspacePath, "home", ".openclaw", "workspace", "skills")
	}
	if strings.EqualFold(strings.TrimSpace(instance.Type), RuntimeTypeHermes) {
		return filepath.Join(workspacePath, ".hermes", "skills")
	}
	return filepath.Join(workspacePath, "home", ".openclaw", "workspace", "skills")
}

func liteSkillInstallRoot(instance *models.Instance) string {
	return runtimeSkillInstallRoot(instance)
}

func sanitizeWorkspaceRelativePath(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\\", "/")
	value = strings.Trim(value, "/")
	if value == "" || strings.Contains(value, "..") {
		return ""
	}
	parts := make([]string, 0, strings.Count(value, "/")+1)
	for _, part := range strings.Split(value, "/") {
		part = strings.TrimSpace(part)
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") {
			return ""
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 || len(parts) > runtimeSkillDiscoveryMaxDepth {
		return ""
	}
	return strings.Join(parts, "/")
}

func skillKeyFromRelativePath(relativePath string) string {
	relativePath = sanitizeWorkspaceRelativePath(relativePath)
	if relativePath == "" {
		return ""
	}
	parts := strings.Split(relativePath, "/")
	return sanitizeSkillKey(parts[len(parts)-1])
}

func runtimeSkillInstallRelativePath(instance *models.Instance, relativePath string) string {
	relativePath = sanitizeWorkspaceRelativePath(relativePath)
	if relativePath == "" {
		return ""
	}
	if instance == nil || instance.WorkspacePath == nil {
		return relativePath
	}
	workspacePath := filepath.Clean(strings.TrimSpace(*instance.WorkspacePath))
	target := filepath.Join(runtimeSkillInstallRoot(instance), filepath.FromSlash(relativePath))
	rel, err := filepath.Rel(workspacePath, target)
	if err != nil {
		return filepath.ToSlash(relativePath)
	}
	return filepath.ToSlash(rel)
}

func joinRuntimeSkillPath(root, relativePath string) (string, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	relativePath = sanitizeWorkspaceRelativePath(relativePath)
	if root == "" || relativePath == "" {
		return "", filepath.ErrBadPattern
	}
	target := filepath.Join(root, filepath.FromSlash(relativePath))
	if !isPathWithin(root, target) {
		return "", filepath.ErrBadPattern
	}
	return target, nil
}

func discoverRuntimeSkillDirectories(root string, maxDepth int) ([]runtimeSkillDiscovery, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" {
		return nil, nil
	}
	if maxDepth <= 0 {
		maxDepth = runtimeSkillDiscoveryMaxDepth
	}
	result := make([]runtimeSkillDiscovery, 0)
	var walk func(currentRoot, relativePrefix string, depth int) error
	walk = func(currentRoot, relativePrefix string, depth int) error {
		entries, err := readDirNames(currentRoot)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			name := strings.TrimSpace(entry.Name)
			if name == "" || strings.HasPrefix(name, ".") || name == ".tmp" {
				continue
			}
			if !entry.IsDir {
				continue
			}
			skillRoot := filepath.Join(currentRoot, name)
			relativePath := name
			if relativePrefix != "" {
				relativePath = relativePrefix + "/" + name
			}
			relativePath = sanitizeWorkspaceRelativePath(relativePath)
			if relativePath == "" {
				continue
			}
			files, err := collectLiteSkillDirectoryFiles(skillRoot)
			if err != nil {
				return err
			}
			if len(files) > 0 {
				result = append(result, runtimeSkillDiscovery{
					RelativePath: relativePath,
					SkillRoot:    skillRoot,
				})
				continue
			}
			if depth < maxDepth {
				if err := walk(skillRoot, relativePath, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(root, "", 1); err != nil {
		return nil, err
	}
	return result, nil
}

func readDirNames(path string) ([]dirEntryName, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	result := make([]dirEntryName, 0, len(entries))
	for _, entry := range entries {
		result = append(result, dirEntryName{Name: entry.Name(), IsDir: entry.IsDir()})
	}
	return result, nil
}

type dirEntryName struct {
	Name  string
	IsDir bool
}
