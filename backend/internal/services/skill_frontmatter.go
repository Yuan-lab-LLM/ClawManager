package services

import (
	"fmt"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

type skillFrontmatter struct {
	Description interface{} `yaml:"description"`
}

func findSkillManifestBytes(files map[string][]byte) ([]byte, bool) {
	for name, content := range files {
		if strings.EqualFold(path.Base(normalizeArchiveEntryPath(name)), "SKILL.md") {
			return content, true
		}
	}
	return nil, false
}

func extractSkillFrontmatterDescription(skillMD []byte) string {
	text := strings.ReplaceAll(string(skillMD), "\r\n", "\n")
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "---") {
		return ""
	}
	rest := trimmed[3:]
	if strings.HasPrefix(rest, "\n") {
		rest = rest[1:]
	}
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return ""
	}
	yamlBlock := rest[:end]
	var meta skillFrontmatter
	if err := yaml.Unmarshal([]byte(yamlBlock), &meta); err != nil {
		return ""
	}
	return normalizeFrontmatterDescription(meta.Description)
}

func normalizeFrontmatterDescription(value interface{}) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case []interface{}:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" && text != "<nil>" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	default:
		text := strings.TrimSpace(fmt.Sprint(typed))
		if text == "" || text == "<nil>" {
			return ""
		}
		return text
	}
}

func descriptionFromSkillFiles(files map[string][]byte) *string {
	content, ok := findSkillManifestBytes(files)
	if !ok {
		return nil
	}
	desc := extractSkillFrontmatterDescription(content)
	if desc == "" {
		return nil
	}
	return &desc
}

func skillMarkdownFromArchive(filename string, archive []byte) (string, error) {
	dirs, err := extractSkillDirectories(filename, archive)
	if err != nil {
		return "", err
	}
	if len(dirs) == 0 {
		return "", fmt.Errorf("skill package has no skill directory")
	}
	content, ok := findSkillManifestBytes(dirs[0].Files)
	if !ok || len(content) == 0 {
		return "", fmt.Errorf("SKILL.md not found")
	}
	return string(content), nil
}

func descriptionFromArchive(filename string, archive []byte) (*string, error) {
	markdown, err := skillMarkdownFromArchive(filename, archive)
	if err != nil {
		return nil, err
	}
	desc := extractSkillFrontmatterDescription([]byte(markdown))
	if desc == "" {
		return nil, nil
	}
	return &desc, nil
}
