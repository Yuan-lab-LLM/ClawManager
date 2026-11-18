package config

import "testing"

func TestKubernetesAPIRateEnvironment(t *testing.T) {
	t.Setenv("K8S_API_QPS", "75")
	t.Setenv("K8S_API_BURST", "150")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kubernetes.APIQPS != 75 || cfg.Kubernetes.APIBurst != 150 {
		t.Fatal("Kubernetes API override lost")
	}
}
