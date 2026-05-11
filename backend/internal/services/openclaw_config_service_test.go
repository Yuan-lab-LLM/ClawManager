package services

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"clawreef/internal/models"
)

func TestRenderCompiledOpenClawPayloadRendersChannelsAsKeyedConfigMap(t *testing.T) {
	t.Parallel()

	resources := []compiledOpenClawResource{
		{
			model: models.OpenClawConfigResource{
				ID:           1,
				ResourceType: OpenClawConfigResourceTypeChannel,
				ResourceKey:  "dingtalk-connector",
				Name:         "DingTalk",
				Version:      1,
				ContentJSON:  `{"schemaVersion":1,"kind":"channel","format":"channel/dingtalk-connector@v1","dependsOn":[],"config":{"enabled":false,"clientId":"ding-xxxxxxxxxxxxxx","clientSecret":"xxxxxxxxxxxxxxxxxxxxxxx","allowFrom":["*"],"legacyField":"drop-me"}}`,
			},
			envelope: OpenClawConfigEnvelope{
				SchemaVersion: 1,
				Kind:          "channel",
				Format:        "channel/dingtalk-connector@v1",
				Config:        json.RawMessage(`{"enabled":false,"clientId":"ding-xxxxxxxxxxxxxx","clientSecret":"xxxxxxxxxxxxxxxxxxxxxxx","allowFrom":["*"],"legacyField":"drop-me"}`),
			},
		},
		{
			model: models.OpenClawConfigResource{
				ID:           2,
				ResourceType: OpenClawConfigResourceTypeChannel,
				ResourceKey:  "feishu",
				Name:         "Feishu",
				Version:      1,
				ContentJSON:  `{"schemaVersion":1,"kind":"channel","format":"channel/feishu@v1","dependsOn":[],"config":{"enabled":false,"domain":"feishu","appId":"cli_top","appSecret":"top_secret","defaultAccount":"default","accounts":{"default":{"appId":"cli_xxx","appSecret":"xxx","botName":"old-bot","enabled":true}},"requireMention":true}}`,
			},
			envelope: OpenClawConfigEnvelope{
				SchemaVersion: 1,
				Kind:          "channel",
				Format:        "channel/feishu@v1",
				Config:        json.RawMessage(`{"enabled":false,"domain":"feishu","appId":"cli_top","appSecret":"top_secret","defaultAccount":"default","accounts":{"default":{"appId":"cli_xxx","appSecret":"xxx","botName":"old-bot","enabled":true}},"requireMention":true}`),
			},
		},
		{
			model: models.OpenClawConfigResource{
				ID:           3,
				ResourceType: OpenClawConfigResourceTypeChannel,
				ResourceKey:  "slack",
				Name:         "Slack",
				Version:      1,
				ContentJSON:  `{"schemaVersion":1,"kind":"channel","format":"channel/slack@v1","dependsOn":[],"config":{"enabled":false,"botToken":"xoxb-xxx","appToken":"xapp-xxx","groupPolicy":"allowlist","channels":{"#general":{"allow":true}},"capabilities":{"interactiveReplies":true}}}`,
			},
			envelope: OpenClawConfigEnvelope{
				SchemaVersion: 1,
				Kind:          "channel",
				Format:        "channel/slack@v1",
				Config:        json.RawMessage(`{"enabled":false,"botToken":"xoxb-xxx","appToken":"xapp-xxx","groupPolicy":"allowlist","channels":{"#general":{"allow":true}},"capabilities":{"interactiveReplies":true}}`),
			},
		},
		{
			model: models.OpenClawConfigResource{
				ID:           4,
				ResourceType: OpenClawConfigResourceTypeChannel,
				ResourceKey:  "telegram",
				Name:         "Telegram",
				Version:      1,
				ContentJSON:  `{"schemaVersion":1,"kind":"channel","format":"channel/telegram@v1","dependsOn":[],"config":{"enabled":false,"botToken":"123456:xxx","dmPolicy":"open","allowFrom":["*"],"network":{"autoSelectFamily":false}}}`,
			},
			envelope: OpenClawConfigEnvelope{
				SchemaVersion: 1,
				Kind:          "channel",
				Format:        "channel/telegram@v1",
				Config:        json.RawMessage(`{"enabled":false,"botToken":"123456:xxx","dmPolicy":"open","allowFrom":["*"],"network":{"autoSelectFamily":false}}`),
			},
		},
		{
			model: models.OpenClawConfigResource{
				ID:           5,
				ResourceType: OpenClawConfigResourceTypeSkill,
				ResourceKey:  "support-bot",
				Name:         "Support Bot",
				Version:      1,
				ContentJSON:  `{"schemaVersion":1,"kind":"skill","format":"skill/custom@v1","dependsOn":[],"config":{"prompt":"help"}}`,
			},
			tags: []string{"skill"},
			envelope: OpenClawConfigEnvelope{
				SchemaVersion: 1,
				Kind:          "skill",
				Format:        "skill/custom@v1",
				Config:        json.RawMessage(`{"prompt":"help"}`),
			},
		},
	}

	renderedEnv, _, _, _, err := renderCompiledOpenClawPayload(OpenClawConfigPlan{Mode: OpenClawConfigPlanModeManual}, nil, resources)
	if err != nil {
		t.Fatalf("renderCompiledOpenClawPayload returned error: %v", err)
	}

	gotChannels := renderedEnv[OpenClawChannelsEnv]
	wantChannels := `{"dingtalk-connector":{"allowFrom":["*"],"clientId":"ding-xxxxxxxxxxxxxx","clientSecret":"xxxxxxxxxxxxxxxxxxxxxxx","enabled":true},"feishu":{"accounts":{"main":{"appId":"cli_xxx","appSecret":"xxx"}},"enabled":true},"slack":{"appToken":"xapp-xxx","botToken":"xoxb-xxx","capabilities":{"interactiveReplies":true},"channels":{"#general":{"allow":true}},"enabled":true,"groupPolicy":"allowlist"},"telegram":{"allowFrom":["*"],"botToken":"123456:xxx","dmPolicy":"open","enabled":true}}`
	if gotChannels != wantChannels {
		t.Fatalf("unexpected channel payload:\nwant: %s\ngot:  %s", wantChannels, gotChannels)
	}

	gotSkills := renderedEnv[OpenClawSkillsEnv]
	wantSkills := `{"items":[{"content":{"schemaVersion":1,"kind":"skill","format":"skill/custom@v1","dependsOn":[],"config":{"prompt":"help"}},"id":5,"key":"support-bot","name":"Support Bot","tags":["skill"],"type":"skill","version":1}],"schemaVersion":1}`
	if gotSkills != wantSkills {
		t.Fatalf("unexpected skill payload:\nwant: %s\ngot:  %s", wantSkills, gotSkills)
	}
}

func TestRuntimeBootstrapEnvValuesAddsHermesAndRuntimeAliases(t *testing.T) {
	env := map[string]string{
		OpenClawChannelsEnv:          `{"slack":{"enabled":true}}`,
		OpenClawSkillsEnv:            `{"schemaVersion":1,"items":[]}`,
		OpenClawBootstrapManifestEnv: `{"schemaVersion":1,"payloads":[{"env":"CLAWMANAGER_OPENCLAW_CHANNELS_JSON","count":1},{"env":"CLAWMANAGER_OPENCLAW_SKILLS_JSON","count":0}]}`,
	}

	got := runtimeBootstrapEnvValues("hermes", env)

	if got[HermesChannelsEnv] != env[OpenClawChannelsEnv] {
		t.Fatalf("expected Hermes channels alias to mirror OpenClaw channels")
	}
	if got[RuntimeSkillsEnv] != env[OpenClawSkillsEnv] {
		t.Fatalf("expected runtime skills alias to mirror OpenClaw skills")
	}
	if got[OpenClawChannelsEnv] != env[OpenClawChannelsEnv] {
		t.Fatalf("expected original OpenClaw channels env to be preserved")
	}
	if !strings.Contains(got[HermesBootstrapManifestEnv], HermesChannelsEnv) {
		t.Fatalf("expected Hermes manifest alias to reference Hermes env names, got %s", got[HermesBootstrapManifestEnv])
	}
	if !strings.Contains(got[RuntimeBootstrapManifestEnv], RuntimeChannelsEnv) {
		t.Fatalf("expected runtime manifest alias to reference runtime env names, got %s", got[RuntimeBootstrapManifestEnv])
	}
}

func TestResourcePayloadFromModelNormalizesStoredChannelJSON(t *testing.T) {
	t.Parallel()

	item := models.OpenClawConfigResource{
		ID:           10,
		UserID:       1,
		ResourceType: OpenClawConfigResourceTypeChannel,
		ResourceKey:  "slack",
		Name:         "Slack",
		Enabled:      true,
		Version:      1,
		TagsJSON:     `["channel","builtin","slack"]`,
		ContentJSON:  `{"schemaVersion":1,"kind":"channel","format":"channel/slack@v1","dependsOn":[],"config":{"enabled":false,"botToken":"xoxb-xxxxxxxxx","appToken":"xapp-xxxxxxxxxxxxxx","groupPolicy":"allowlist","channels":{"#general":{"allow":true}},"capabilities":{"interactiveReplies":true},"legacyField":"drop-me"}}`,
	}

	payload, err := resourcePayloadFromModel(item)
	if err != nil {
		t.Fatalf("resourcePayloadFromModel returned error: %v", err)
	}

	got := string(payload.Content)
	want := `{"schemaVersion":1,"kind":"channel","format":"channel/slack@v1","dependsOn":[],"config":{"enabled":true,"botToken":"xoxb-xxxxxxxxx","appToken":"xapp-xxxxxxxxxxxxxx","groupPolicy":"allowlist","channels":{"#general":{"allow":true}},"capabilities":{"interactiveReplies":true},"legacyField":"drop-me"}}`

	var gotJSON interface{}
	if err := json.Unmarshal([]byte(got), &gotJSON); err != nil {
		t.Fatalf("failed to unmarshal normalized resource content: %v", err)
	}

	var wantJSON interface{}
	if err := json.Unmarshal([]byte(want), &wantJSON); err != nil {
		t.Fatalf("failed to unmarshal expected normalized resource content: %v", err)
	}

	if !reflect.DeepEqual(gotJSON, wantJSON) {
		t.Fatalf("unexpected normalized resource content:\nwant: %s\ngot:  %s", want, got)
	}
}

func TestResourcePayloadFromModelNormalizesStoredDingTalkChannelJSON(t *testing.T) {
	t.Parallel()

	item := models.OpenClawConfigResource{
		ID:           11,
		UserID:       1,
		ResourceType: OpenClawConfigResourceTypeChannel,
		ResourceKey:  "dingtalk-connector",
		Name:         "DingTalk",
		Enabled:      true,
		Version:      1,
		TagsJSON:     `["channel","builtin","dingtalk-connector"]`,
		ContentJSON:  `{"schemaVersion":1,"kind":"channel","format":"channel/dingtalk-connector@v1","dependsOn":[],"config":{"enabled":false,"clientId":"ding-xxxxxxxxxxxxxx","clientSecret":"xxxxxxxxxxxxxxxxxxxxxxx","allowFrom":[],"legacyField":"drop-me"}}`,
	}

	payload, err := resourcePayloadFromModel(item)
	if err != nil {
		t.Fatalf("resourcePayloadFromModel returned error: %v", err)
	}

	got := string(payload.Content)
	want := `{"schemaVersion":1,"kind":"channel","format":"channel/dingtalk-connector@v1","dependsOn":[],"config":{"enabled":true,"clientId":"ding-xxxxxxxxxxxxxx","clientSecret":"xxxxxxxxxxxxxxxxxxxxxxx","allowFrom":["*"],"legacyField":"drop-me"}}`

	var gotJSON interface{}
	if err := json.Unmarshal([]byte(got), &gotJSON); err != nil {
		t.Fatalf("failed to unmarshal normalized resource content: %v", err)
	}

	var wantJSON interface{}
	if err := json.Unmarshal([]byte(want), &wantJSON); err != nil {
		t.Fatalf("failed to unmarshal expected normalized resource content: %v", err)
	}

	if !reflect.DeepEqual(gotJSON, wantJSON) {
		t.Fatalf("unexpected normalized resource content:\nwant: %s\ngot:  %s", want, got)
	}
}

func intPtr(v int) *int { return &v }

func TestSnapshotReferencesResource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		snap       models.OpenClawInjectionSnapshot
		resourceID int
		want       bool
	}{
		{
			name:       "matching ID in SelectedResourceIDsJSON",
			snap:       models.OpenClawInjectionSnapshot{SelectedResourceIDsJSON: `[8, 15, 22]`},
			resourceID: 15,
			want:       true,
		},
		{
			name:       "single matching ID",
			snap:       models.OpenClawInjectionSnapshot{SelectedResourceIDsJSON: `[8]`},
			resourceID: 8,
			want:       true,
		},
		{
			name:       "no match",
			snap:       models.OpenClawInjectionSnapshot{SelectedResourceIDsJSON: `[8, 15, 22]`},
			resourceID: 99,
			want:       false,
		},
		{
			name:       "empty JSON",
			snap:       models.OpenClawInjectionSnapshot{SelectedResourceIDsJSON: ""},
			resourceID: 1,
			want:       false,
		},
		{
			name:       "whitespace only",
			snap:       models.OpenClawInjectionSnapshot{SelectedResourceIDsJSON: "   "},
			resourceID: 1,
			want:       false,
		},
		{
			name:       "invalid JSON",
			snap:       models.OpenClawInjectionSnapshot{SelectedResourceIDsJSON: `broken`},
			resourceID: 1,
			want:       false,
		},
		// v2: ResolvedResourcesJSON cases
		{
			name: "matching ID only in ResolvedResourcesJSON (indirect dependency)",
			snap: models.OpenClawInjectionSnapshot{
				SelectedResourceIDsJSON: `[10]`,
				ResolvedResourcesJSON:   `[{"id":10,"type":"agent","key":"bot","name":"Bot","version":1},{"id":20,"type":"skill","key":"helper","name":"Helper","version":1}]`,
			},
			resourceID: 20,
			want:       true,
		},
		{
			name: "no match in ResolvedResourcesJSON",
			snap: models.OpenClawInjectionSnapshot{
				SelectedResourceIDsJSON: `[10]`,
				ResolvedResourcesJSON:   `[{"id":10,"type":"agent","key":"bot","name":"Bot","version":1}]`,
			},
			resourceID: 99,
			want:       false,
		},
		{
			name: "ResolvedResourcesJSON malformed — fallback to SelectedResourceIDsJSON succeeds",
			snap: models.OpenClawInjectionSnapshot{
				SelectedResourceIDsJSON: `[5, 10]`,
				ResolvedResourcesJSON:   `not-valid-json`,
			},
			resourceID: 10,
			want:       true,
		},
		{
			name: "ResolvedResourcesJSON malformed — fallback to SelectedResourceIDsJSON no match",
			snap: models.OpenClawInjectionSnapshot{
				SelectedResourceIDsJSON: `[5, 10]`,
				ResolvedResourcesJSON:   `not-valid-json`,
			},
			resourceID: 99,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := snapshotReferencesResource(tt.snap, tt.resourceID)
			if got != tt.want {
				t.Errorf("snapshotReferencesResource() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlanFromSnapshot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		snap    models.OpenClawInjectionSnapshot
		want    OpenClawConfigPlan
		wantErr bool
	}{
		{
			name: "bundle mode",
			snap: models.OpenClawInjectionSnapshot{
				Mode:                    "bundle",
				BundleID:                intPtr(5),
				SelectedResourceIDsJSON: `[8, 15]`,
			},
			want: OpenClawConfigPlan{
				Mode:     "bundle",
				BundleID: intPtr(5),
			},
		},
		{
			name: "manual mode",
			snap: models.OpenClawInjectionSnapshot{
				Mode:                    "manual",
				SelectedResourceIDsJSON: `[8, 15, 22]`,
			},
			want: OpenClawConfigPlan{
				Mode:        "manual",
				ResourceIDs: []int{8, 15, 22},
			},
		},
		{
			name: "manual mode with invalid JSON",
			snap: models.OpenClawInjectionSnapshot{
				Mode:                    "manual",
				SelectedResourceIDsJSON: `broken`,
			},
			wantErr: true,
		},
		{
			name: "none mode",
			snap: models.OpenClawInjectionSnapshot{
				Mode: "none",
			},
			want: OpenClawConfigPlan{
				Mode: "none",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := planFromSnapshot(tt.snap)
			if (err != nil) != tt.wantErr {
				t.Fatalf("planFromSnapshot() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("planFromSnapshot() = %+v, want %+v", got, tt.want)
			}
		})
	}

}

// TestNormalizeOpenClawResourceContentPreservesUnknownChannelFields asserts
// that write-time normalization keeps tenant-authored keys that the runtime
// env-render allowlist does not surface, for every supported channel editor.
// Regression guard for the "save drops unknown fields" data-loss bug.
func TestNormalizeOpenClawResourceContentPreservesUnknownChannelFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		resourceKey string
		content     string
		wantKeep    map[string]interface{}
	}{
		{
			name:        "telegram preserves webhook and custom capabilities",
			resourceKey: "telegram",
			content:     `{"schemaVersion":1,"kind":"channel","format":"channel/telegram@v1","dependsOn":[],"config":{"enabled":true,"botToken":"123:abc","dmPolicy":"open","allowFrom":["*"],"webhook":"https://example.com/hook","capabilities":{"interactiveReplies":true}}}`,
			wantKeep: map[string]interface{}{
				"webhook":      "https://example.com/hook",
				"capabilities": map[string]interface{}{"interactiveReplies": true},
			},
		},
		{
			name:        "dingtalk-connector preserves dmPolicy and webhook",
			resourceKey: "dingtalk-connector",
			content:     `{"schemaVersion":1,"kind":"channel","format":"channel/dingtalk-connector@v1","dependsOn":[],"config":{"enabled":true,"clientId":"ding-x","clientSecret":"sec","allowFrom":["*"],"dmPolicy":"closed","webhook":"https://example.com/hook"}}`,
			wantKeep: map[string]interface{}{
				"dmPolicy": "closed",
				"webhook":  "https://example.com/hook",
			},
		},
		{
			name:        "slack preserves allowFrom and dmPolicy",
			resourceKey: "slack",
			content:     `{"schemaVersion":1,"kind":"channel","format":"channel/slack@v1","dependsOn":[],"config":{"enabled":true,"botToken":"xoxb-x","appToken":"xapp-x","groupPolicy":"allowlist","channels":{"#general":{"allow":true}},"capabilities":{"interactiveReplies":true},"allowFrom":["C123","C456"],"dmPolicy":"closed"}}`,
			wantKeep: map[string]interface{}{
				"allowFrom": []interface{}{"C123", "C456"},
				"dmPolicy":  "closed",
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			normalized, err := normalizeOpenClawResourceContent(OpenClawConfigResourceTypeChannel, tc.resourceKey, json.RawMessage(tc.content))
			if err != nil {
				t.Fatalf("normalizeOpenClawResourceContent returned error: %v", err)
			}
			var env OpenClawConfigEnvelope
			if err := json.Unmarshal(normalized, &env); err != nil {
				t.Fatalf("failed to unmarshal normalized envelope: %v", err)
			}
			var config map[string]interface{}
			if err := json.Unmarshal(env.Config, &config); err != nil {
				t.Fatalf("failed to unmarshal normalized config: %v", err)
			}
			for k, want := range tc.wantKeep {
				got, ok := config[k]
				if !ok {
					t.Fatalf("expected key %q to survive normalization, got dropped; full config: %s", k, env.Config)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("key %q mismatched after normalization:\nwant: %v\ngot:  %v", k, want, got)
				}
			}
		})
	}
}

// TestNormalizeOpenClawResourceContentPreservesFeishuSiblingAccounts asserts
// the deep-merge behavior of the feishu accounts map: accounts other than
// "main" survive, and sibling fields inside accounts.main survive too. The
// main account's allowlisted fields (appId, appSecret) are still normalized.
func TestNormalizeOpenClawResourceContentPreservesFeishuSiblingAccounts(t *testing.T) {
	t.Parallel()

	input := `{"schemaVersion":1,"kind":"channel","format":"channel/feishu@v1","dependsOn":[],"config":{"enabled":true,"accounts":{"main":{"appId":"cli_main","appSecret":"main_sec","verificationToken":"vt_keep"},"default":{"appId":"cli_default","appSecret":"default_sec"},"acme":{"appId":"cli_acme","appSecret":"acme_sec","botName":"acme-bot"}},"requireMention":true}}`

	normalized, err := normalizeOpenClawResourceContent(OpenClawConfigResourceTypeChannel, "feishu", json.RawMessage(input))
	if err != nil {
		t.Fatalf("normalizeOpenClawResourceContent returned error: %v", err)
	}

	var env OpenClawConfigEnvelope
	if err := json.Unmarshal(normalized, &env); err != nil {
		t.Fatalf("failed to unmarshal envelope: %v", err)
	}
	var config map[string]interface{}
	if err := json.Unmarshal(env.Config, &config); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	if got := config["requireMention"]; got != true {
		t.Fatalf("expected top-level requireMention=true to survive, got %v", got)
	}

	accounts, ok := config["accounts"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected accounts map, got %T", config["accounts"])
	}

	wantDefault := map[string]interface{}{"appId": "cli_default", "appSecret": "default_sec"}
	if !reflect.DeepEqual(accounts["default"], wantDefault) {
		t.Fatalf("default account not preserved:\nwant: %v\ngot:  %v", wantDefault, accounts["default"])
	}
	wantAcme := map[string]interface{}{"appId": "cli_acme", "appSecret": "acme_sec", "botName": "acme-bot"}
	if !reflect.DeepEqual(accounts["acme"], wantAcme) {
		t.Fatalf("acme account not preserved:\nwant: %v\ngot:  %v", wantAcme, accounts["acme"])
	}

	main, ok := accounts["main"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected accounts.main map, got %T", accounts["main"])
	}
	if main["verificationToken"] != "vt_keep" {
		t.Fatalf("expected accounts.main.verificationToken to survive, got %v", main["verificationToken"])
	}
	if main["appId"] != "cli_main" {
		t.Fatalf("expected accounts.main.appId normalized to cli_main, got %v", main["appId"])
	}
	if main["appSecret"] != "main_sec" {
		t.Fatalf("expected accounts.main.appSecret normalized to main_sec, got %v", main["appSecret"])
	}
}
