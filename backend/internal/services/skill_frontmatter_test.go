package services

import (
	"strings"
	"testing"
)

func TestExtractSkillFrontmatterDescription(t *testing.T) {
	md := []byte(`---
name: demo
description: 用于演示的技能简介
---
# Body
hello
`)
	got := extractSkillFrontmatterDescription(md)
	if got != "用于演示的技能简介" {
		t.Fatalf("description = %q", got)
	}

	folded := []byte(`---
description: >
  line one
  line two
---
`)
	got = extractSkillFrontmatterDescription(folded)
	if !strings.Contains(got, "line one") || !strings.Contains(got, "line two") {
		t.Fatalf("folded description = %q", got)
	}

	if extractSkillFrontmatterDescription([]byte("# no frontmatter\n")) != "" {
		t.Fatal("expected empty without frontmatter")
	}
	if extractSkillFrontmatterDescription([]byte("---\nname: x\n---\n")) != "" {
		t.Fatal("expected empty without description field")
	}
}

func TestDescriptionFromSkillFiles(t *testing.T) {
	files := map[string][]byte{
		"SKILL.md": []byte("---\ndescription: hello world\n---\n# Title\n"),
	}
	got := descriptionFromSkillFiles(files)
	if got == nil || *got != "hello world" {
		t.Fatalf("descriptionFromSkillFiles = %#v", got)
	}
	if descriptionFromSkillFiles(map[string][]byte{"README.md": []byte("x")}) != nil {
		t.Fatal("expected nil without SKILL.md")
	}
}
