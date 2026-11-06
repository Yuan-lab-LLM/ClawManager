package main

import (
	"strings"
	"testing"
)

func TestHashInputStrictAndBounded(t *testing.T) {
	valid := `{"metadata":{"labels":{"app":"test"}},"spec":{"containers":[{"name":"runtime","image":"image"}]}}`
	for _, raw := range []string{`null`, `{}`, valid + ` {}`, strings.Replace(valid, `"image":"image"`, `"image":"image","unknown":true`, 1), strings.Repeat(" ", maxTemplateBytes+1)} {
		if _, err := hashTemplate([]byte(raw)); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	hash, err := hashTemplate([]byte(valid))
	if err != nil || hash == "" {
		t.Fatalf("valid template rejected: %v", err)
	}
	changed, err := hashTemplate([]byte(strings.Replace(valid, `"image":"image"`, `"image":"other"`, 1)))
	if err != nil || changed == hash {
		t.Fatal("template edit did not change hash")
	}
	labelled := strings.Replace(valid, `"app":"test"`, `"app":"test","pod-template-hash":"irrelevant"`, 1)
	same, err := hashTemplate([]byte(labelled))
	if err != nil || same != hash {
		t.Fatal("controller-added hash label affected original hash")
	}
}
