import fs from 'node:fs';
import path from 'node:path';
import {execFileSync} from 'node:child_process';
const root=path.resolve(process.argv[2]||''),exe=process.argv[3];
if(root.toLowerCase()!=='d:\\agentsruntime'||!exe)throw new Error('Expected exact AgentsRuntime root and apply-patch executable');
function patch(file,before,after){
 const target=path.join(root,file).replaceAll('\\','/');
 const old=fs.existsSync(target)?fs.readFileSync(target,'utf8').replaceAll('\r\n','\n'):null;
 if(before!==null && (old?.includes(after) || (file.endsWith('.go') && old?.replace(/\s/g,'').includes(after.replace(/\s/g,'')))))return;
 if(before===null && old!==null){if(old.trimEnd()===after.trimEnd()||(file.endsWith('.go')&&old.replace(/\s/g,'')===after.replace(/\s/g,'')))return;throw new Error('Existing file differs: '+file);}
 let next=after;
 if(before!==null){if(old.split(before).length!==2)throw new Error('Source mismatch: '+file);next=old.replace(before,after);}
 let body;
 if(old!==null){
  const a=old.trimEnd().split('\n'),b=next.trimEnd().split('\n');let first=0,ea=a.length,eb=b.length;
  while(first<ea&&first<eb&&a[first]===b[first])first++;
  while(ea>first&&eb>first&&a[ea-1]===b[eb-1]){ea--;eb--;}
  first=Math.max(0,first-2);ea=Math.min(a.length,ea+2);eb=Math.min(b.length,eb+2);
  body=`*** Update File: ${target}\n@@\n`+a.slice(first,ea).map(x=>'-'+x).join('\n')+'\n'+b.slice(first,eb).map(x=>'+'+x).join('\n');
 }else body=`*** Add File: ${target}\n`+next.trimEnd().split('\n').map(x=>'+'+x).join('\n');
 try{execFileSync(exe,['--codex-run-as-apply-patch','*** Begin Patch\n'+body+'\n*** End Patch'],{stdio:'inherit'});}catch(e){throw new Error(`Patch failed: ${file} (${e.code??e.status})`);}
}
const agent='clawmanager-agent/';
patch(agent+'internal/runtime/opencode/config_writer.go','["modalities"] = map[string]any','["capabilities"] = map[string]any');
patch(agent+'internal/runtime/opencode/vision_test.go','strings.Contains(string(raw), "modalities")','strings.Contains(string(raw), "capabilities")');
patch(agent+'go.mod','go 1.26','go 1.26\n\nrequire gopkg.in/yaml.v3 v3.0.1');
patch(agent+'internal/runtime/hermes/config_writer.go','\treturn resolved, nil\n}',`\tvision, err := gateway.ResolveVisionCapabilities(cfg, req)
    if err != nil { return gateway.Config{}, err }
    resolved.LLMVision = vision
\treturn resolved, nil
}`);
patch(agent+'internal/runtime/hermes/config_writer.go','\tbase := stripManagedBlock(string(existing))',`\tbase := stripManagedBlock(string(existing))
    base, err = mergeManagedVisionYAML(base, cfg)
    if err != nil { return err }`);
patch(agent+'internal/runtime/hermes/vision.go',null,`package hermes
import (
 "fmt"
 "strings"
 "gopkg.in/yaml.v3"
 "github.com/iamlovingit/clawmanager-agent/internal/gateway"
)

// Pin only the vision auxiliary task. Preserve other auxiliary settings,
// comments and unrelated user configuration. Legacy requests remain unchanged.
func mergeManagedVisionYAML(base string, cfg gateway.Config) (string,error) {
 if cfg.LLMVision==nil { return base,nil }
 if strings.TrimSpace(cfg.LLMBaseURL)=="" { return "",fmt.Errorf("managed vision requires gateway base URL") }
 var doc yaml.Node
 if err:=yaml.Unmarshal([]byte(base),&doc);err!=nil {return "",fmt.Errorf("parse Hermes vision config: %w",err)}
 if len(doc.Content)==0 {doc.Kind=yaml.DocumentNode;doc.Content=[]*yaml.Node{{Kind:yaml.MappingNode,Tag:"!!map"}}}
 aux,err:=visionYAMLMap(doc.Content[0],"auxiliary");if err!=nil{return "",err}
 vision,err:=visionYAMLMap(aux,"vision");if err!=nil{return "",err}
 for _,pair:=range [][2]string{{"provider","custom"},{"model","auto"},{"base_url",cfg.LLMBaseURL},{"api_key",""},{"api_mode","chat_completions"}} {
  visionYAMLSet(vision,pair[0],&yaml.Node{Kind:yaml.ScalarNode,Tag:"!!str",Value:pair[1]})
 }
 // Do not let a historical extra_body override model or routing parameters.
 visionYAMLSet(vision,"extra_body",&yaml.Node{Kind:yaml.MappingNode,Tag:"!!map"})
 raw,err:=yaml.Marshal(&doc);if err!=nil{return "",err};return string(raw),nil
}
func visionYAMLMap(parent *yaml.Node,key string)(*yaml.Node,error) {
 if parent.Kind!=yaml.MappingNode{return nil,fmt.Errorf("Hermes vision parent must be a mapping")}
 for i:=0;i<len(parent.Content);i+=2 {if parent.Content[i].Value==key {
  value:=parent.Content[i+1];if value.Kind!=yaml.MappingNode {return nil,fmt.Errorf("Hermes %s must be a mapping",key)};return value,nil
 }}
 value:=&yaml.Node{Kind:yaml.MappingNode,Tag:"!!map"};visionYAMLSet(parent,key,value);return value,nil
}
func visionYAMLSet(parent *yaml.Node,key string,value *yaml.Node) {
 for i:=0;i<len(parent.Content);i+=2 {if parent.Content[i].Value==key {parent.Content[i+1]=value;return}}
 parent.Content=append(parent.Content,&yaml.Node{Kind:yaml.ScalarNode,Tag:"!!str",Value:key},value)
}
`);
patch(agent+'internal/runtime/hermes/vision_test.go',null,`package hermes
import("testing";"strings";"gopkg.in/yaml.v3";"github.com/iamlovingit/clawmanager-agent/internal/gateway")
func TestManagedVisionKeepsUnrelatedAuxiliaryConfiguration(t *testing.T){
 base:="# user comment\\nauxiliary:\\n  compression:\\n    model: keep-me\\n  vision:\\n    provider: openrouter\\n    api_key: old-key\\n    timeout: 42\\n    extra_body:\\n      model: unauthorized\\nterminal:\\n  timeout: 500\\n"
 cfg:=gateway.Config{LLMBaseURL:"https://gateway.example/llm",LLMVision:map[string]bool{"auto":true}}
 got,err:=mergeManagedVisionYAML(base,cfg);if err!=nil{t.Fatal(err)}
 var parsed map[string]any;if err:=yaml.Unmarshal([]byte(got),&parsed);err!=nil{t.Fatal(err)}
 aux:=parsed["auxiliary"].(map[string]any);vision:=aux["vision"].(map[string]any)
 if vision["base_url"]!=cfg.LLMBaseURL||vision["model"]!="auto"||vision["provider"]!="custom"||vision["api_key"]!="" {t.Fatal(vision)}
 if vision["timeout"]!=42||!strings.Contains(got,"keep-me")||!strings.Contains(got,"# user comment")||!strings.Contains(got,"500")||strings.Contains(got,"old-key")||strings.Contains(got,"unauthorized") {t.Fatal(got)}
 again,err:=mergeManagedVisionYAML(got,cfg);if err!=nil||again!=got{t.Fatal("not idempotent")}
 legacy,err:=mergeManagedVisionYAML(base,gateway.Config{});if err!=nil||legacy!=base {t.Fatal("legacy changed")}
 if _,err:=mergeManagedVisionYAML("auxiliary: wrong",cfg);err==nil{t.Fatal("invalid mapping overwritten")}
}
`);
patch('hermes/rootfs/usr/local/bin/hermes-apply-runtime-config','skill_sources = apply_skill_config(data, hermes_home)',`# Managed image analysis must use the same owner-authenticated Gateway.
if "CLAWMANAGER_LLM_VISION" in os.environ:
    vision_flags = json.loads(os.environ["CLAWMANAGER_LLM_VISION"])
    if not isinstance(vision_flags, dict) or any(type(v) is not bool for v in vision_flags.values()):
        raise ValueError("invalid CLAWMANAGER_LLM_VISION")
    if not base_url or not api_key:
        raise ValueError("managed vision requires Gateway URL and token")
    auxiliary = data.setdefault("auxiliary", {})
    if not isinstance(auxiliary, dict):
        raise ValueError("auxiliary must be an object")
    vision = auxiliary.setdefault("vision", {})
    if not isinstance(vision, dict):
        raise ValueError("auxiliary.vision must be an object")
    vision.update(provider="custom", model="auto", base_url=base_url,
                  api_key="", api_mode="chat_completions", extra_body={})

skill_sources = apply_skill_config(data, hermes_home)`);
// Use JSON encoding for Pro config; interpolated shell JSON was unsafe for model names.
patch('opencode/rootfs/usr/local/bin/opencode-apply-runtime-config',
fs.readFileSync(path.join(root,'opencode/rootfs/usr/local/bin/opencode-apply-runtime-config'),'utf8').replaceAll('\r\n','\n').split('cat >"${config_path}" <<EOF\n')[1]?.split('\nEOF\n')[0] ? 'cat >"${config_path}" <<EOF\n'+fs.readFileSync(path.join(root,'opencode/rootfs/usr/local/bin/opencode-apply-runtime-config'),'utf8').replaceAll('\r\n','\n').split('cat >"${config_path}" <<EOF\n')[1].split('\nEOF\n')[0]+'\nEOF' : '__already_applied__',
`python3 - "\x24{config_path}" "\x24{base_url}" "\x24{model_id}" <<'PY'
import json
import os
import sys
from pathlib import Path

target, base_url, selected = sys.argv[1:]
model_ids = [selected, "auto"]
raw_models = os.environ.get("CLAWMANAGER_LLM_MODEL", "").strip()
if raw_models:
    decoded = json.loads(raw_models) if raw_models.startswith("[") else [raw_models]
    if not isinstance(decoded, list) or any(not isinstance(m, str) for m in decoded):
        raise ValueError("invalid model catalogue")
    model_ids.extend(decoded)
models = {m: {"name": "ClawManager Auto" if m == "auto" else m} for m in dict.fromkeys(model_ids)}
raw_vision = os.environ.get("CLAWMANAGER_LLM_VISION", "").strip()
if raw_vision:
    vision = json.loads(raw_vision)
    if not isinstance(vision, dict) or any(type(v) is not bool for v in vision.values()):
        raise ValueError("invalid CLAWMANAGER_LLM_VISION")
    for model, config in models.items():
        if model in vision:
            config["capabilities"] = {"input": ["text", "image"] if vision[model] else ["text"], "output": ["text"]}
doc = {"$schema": "https://opencode.ai/config.json", "model": "clawmanager/" + selected,
       "provider": {"clawmanager": {"npm": "@ai-sdk/openai-compatible", "name": "ClawManager AI Gateway",
           "options": {"baseURL": base_url, "apiKey": "{env:CLAWMANAGER_LLM_API_KEY}"}, "models": models}},
       "enabled_providers": ["clawmanager"],
       "disabled_providers": ["openai", "anthropic", "google", "amazon-bedrock", "azure", "groq", "mistral", "deepseek"]}
destination = Path(target)
temporary = destination.with_suffix(".json.tmp")
temporary.write_text(json.dumps(doc, ensure_ascii=False, indent=2) + "\\n", encoding="utf-8")
temporary.replace(destination)
PY`);
console.log('Applied Hermes auxiliary vision and OpenCode Pro configuration changes; no deployment.');
