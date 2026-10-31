package services

import (
	"context"
	"encoding/json"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net/url"
	"testing"
)

func TestNativeGatewayTargetUsesValidatedAddressAndRuntimePortBlock(t *testing.T) {
	for _, kind := range []string{RuntimeTypeOpenClaw, RuntimeTypeOpenCode, RuntimeTypeHermes, RuntimeTypeDeepSeekHarness} {
		t.Run(kind, func(t *testing.T) {
			a, instances, kube, cm, _ := securityFixture(t)
			instances.instance.Type = kind
			cm.Data["runtime_type"] = kind
			var request RuntimeAgentCreateGatewayRequest
			if err := json.Unmarshal([]byte(cm.Data["request"]), &request); err != nil {
				t.Fatal(err)
			}
			request.AgentType = kind
			request.GatewayPort = 24000
			data, _ := json.Marshal(request)
			cm.Data["request"] = string(data)
			if _, err := kube.CoreV1().ConfigMaps("test").Update(context.Background(), cm, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			for offset := -1; offset <= 3; offset++ {
				target := &url.URL{Scheme: "http", Host: fmt.Sprintf("10.2.0.3:%d", 24000+offset), Path: "/auth/bootstrap"}
				credential, err := a.NativeGatewayCredentialForTarget(context.Background(), 7, target)
				allowed := offset >= 0 && offset < RuntimeGatewayPortBlockSize(kind)
				if allowed && (err != nil || credential == "") || !allowed && (err == nil || credential != "") {
					t.Errorf("offset %d allowed=%v credential=%q error=%v", offset, allowed, credential, err)
				}
			}
			for _, raw := range []string{"http://10.2.0.99:24000/", "http://pool.test:24000/", "http://user:password@10.2.0.3:24000/", "https://10.2.0.3:24000/", "http://10.2.0.3:19090/", "http://[fe80::1%25eth0]:24000/"} {
				target, err := url.Parse(raw)
				if err != nil {
					t.Fatal(err)
				}
				if credential, err := a.NativeGatewayCredentialForTarget(context.Background(), 7, target); err == nil || credential != "" {
					t.Errorf("unsafe target accepted %q", raw)
				}
			}
			if credential, err := a.NativeGatewayCredentialForTarget(context.Background(), 7, nil); err == nil || credential != "" {
				t.Fatal("nil target accepted")
			}
			ambiguous := &url.URL{Scheme: "http", Host: "10.2.0.3:24000", Opaque: "//untrusted.test:24000"}
			if credential, err := a.NativeGatewayCredentialForTarget(context.Background(), 7, ambiguous); err == nil || credential != "" {
				t.Fatal("opaque target can replace the validated host")
			}
		})
	}
}
