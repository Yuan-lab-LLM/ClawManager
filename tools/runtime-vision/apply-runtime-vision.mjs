// Apply narrowly scoped source changes with apply_patch. No builds or deployment.
import fs from 'node:fs';
import path from 'node:path';
import {execFileSync} from 'node:child_process';
const root = path.resolve(process.argv[2] || '');
const executable = process.argv[3];
if (root.toLowerCase() !== 'd:\\agentsruntime' || !executable) throw new Error('Expected D:/AgentsRuntime and apply-patch executable');
const changes = new Map();
function edit(file, before, after) {
  const old = changes.get(file) ?? fs.readFileSync(path.join(root,file),'utf8').replaceAll('\r\n','\n');
  if (old.includes(after) || (file.endsWith('.go') && old.replace(/\s/g,'').includes(after.replace(/\s/g,'')))) return;
  if (old.split(before).length !== 2) throw new Error(`Source mismatch: ${file}: ${before.slice(0,60)}`);
  changes.set(file, old.replace(before,after));
}
function add(file, text) {
  if(fs.existsSync(path.join(root,file))) {
    const existing=fs.readFileSync(path.join(root,file),'utf8').replaceAll('\r\n','\n');
    if(existing===text || (file.endsWith('.go') && existing.replace(/\s/g,'')===text.replace(/\s/g,''))) return;
    throw new Error(`Refusing to replace ${file}`);
  }
  changes.set(file,text);
}
const agent='clawmanager-agent/internal/';
edit(agent+'gateway/types.go', '\tLLMReasoning          map[string]bool', '\tLLMVision             map[string]bool\n\tLLMReasoning          map[string]bool');
add(agent+'gateway/vision.go', `package gateway

import (
 "encoding/json"
 "fmt"
 "strings"
)

// nil means an older manager supplied no capability snapshot. An explicit
// false removes image input; missing entries preserve the previous setting.
func ParseVisionCapabilities(raw string) (map[string]bool, error) {
 if strings.TrimSpace(raw)=="" { return nil,nil }
 var values map[string]json.RawMessage
 if err:=json.Unmarshal([]byte(raw),&values); err!=nil || values==nil { return nil,fmt.Errorf("invalid CLAWMANAGER_LLM_VISION object") }
 result:=make(map[string]bool,len(values))
 for key,value:=range values {
  if string(value)!="true" && string(value)!="false" { return nil,fmt.Errorf("invalid vision capability for %q",key) }
  result[key]=string(value)=="true"
 }
 return result,nil
}

func ResolveVisionCapabilities(cfg Config, req CreateGatewayRequest) (map[string]bool,error) {
 for _,env:=range []map[string]string{req.Environment,req.Env} {
  if raw,ok:=env["CLAWMANAGER_LLM_VISION"]; ok { return ParseVisionCapabilities(raw) }
 }
 return cfg.LLMVision,nil
}

func VisionInputModalities(existing any, enabled bool) []any {
 result:=[]any{}
 add:=func(value any) { if s,ok:=value.(string);ok && !strings.HasPrefix(s,"image") { result=append(result,s) } }
 switch values:=existing.(type) {
 case []any: for _,v:=range values { add(v) }
 case []string: for _,v:=range values { add(v) }
 }
 if len(result)==0 { result=append(result,"text") }
 if enabled { result=append(result,"image") }
 return result
}
`);
edit(agent+'agent/config.go','\tif cfg.ControlToken == "" {',`\tif raw := os.Getenv("CLAWMANAGER_LLM_VISION"); strings.TrimSpace(raw) != "" {
        vision, err := gateway.ParseVisionCapabilities(raw)
        if err != nil { return Config{}, err }
        cfg.LLMVision = vision
    }
\tif cfg.ControlToken == "" {`);
// gateway is already imported by config.go (checked before execution).
edit(agent+'runtime/openclaw/config_writer.go','\treturn resolved, nil\n}',`\tvision, err := gateway.ResolveVisionCapabilities(cfg, req)
    if err != nil { return gateway.Config{}, err }
    resolved.LLMVision = vision
\treturn resolved, nil
}`);
edit(agent+'runtime/openclaw/config_writer.go', 'provider["models"] = buildOpenClawProviderModels(provider["models"], cfg.LLMModelIDs, cfg.LLMReasoning, cfg.LLMReasoningControl)',`provider["models"] = buildOpenClawProviderModels(provider["models"], cfg.LLMModelIDs, cfg.LLMReasoning, cfg.LLMReasoningControl)
        for _, entry := range provider["models"].([]any) {
            model := entry.(map[string]any)
            if vision, managed := cfg.LLMVision[configStringValue(model["id"])]; managed {
                model["input"] = gateway.VisionInputModalities(model["input"], vision)
            }
        }`);
edit(agent+'runtime/opencode/config_writer.go','\tconfigPath := filepath.Join(opencodeHome, "opencode.json")',`\tvision, err := gateway.ResolveVisionCapabilities(cfg, req)
    if err != nil { return err }
\tconfigPath := filepath.Join(opencodeHome, "opencode.json")`);
edit(agent+'runtime/opencode/config_writer.go','"models": modelsMap(models),','"models": modelsMap(models, vision),');
edit(agent+'runtime/opencode/config_writer.go','func modelsMap(models []string) map[string]any {','func modelsMap(models []string, visionMaps ...map[string]bool) map[string]any {');
edit(agent+'runtime/opencode/config_writer.go','\t\tout[id] = map[string]any{"name": name}',`\t\tout[id] = map[string]any{"name": name}
        if len(visionMaps)>0 {
            if vision,managed:=visionMaps[0][id];managed {
                out[id].(map[string]any)["capabilities"] = map[string]any{"input":gateway.VisionInputModalities(nil,vision),"output":[]string{"text"}}
            }
        }`);
add(agent+'gateway/vision_test.go',`package gateway
import("testing"; "reflect")
func TestVisionCapabilitiesAreStrictAndBackwardCompatible(t *testing.T) {
 for _,raw:=range []string{"null","[]",\x60{"auto":null}\x60,\x60{"auto":"true"}\x60} {
  if _,err:=ParseVisionCapabilities(raw);err==nil { t.Fatalf("accepted %s",raw) }
 }
 if got,err:=ParseVisionCapabilities("");got!=nil||err!=nil {t.Fatal("legacy defaults changed")}
 cfg:=Config{LLMVision:map[string]bool{"pool-other-user":true}}
 got,err:=ResolveVisionCapabilities(cfg,CreateGatewayRequest{Environment:map[string]string{"CLAWMANAGER_LLM_VISION":\x60{"auto":false}\x60}})
 if err!=nil||len(got)!=1||got["auto"] {t.Fatal("request capabilities did not replace pool defaults")}
 if got:=VisionInputModalities([]string{"text","audio","image/png"},false);!reflect.DeepEqual(got,[]any{"text","audio"}) {t.Fatal(got)}
}
`);
add(agent+'runtime/opencode/vision_test.go',`package opencode
import("testing"; "encoding/json"; "strings")
func TestVisionModelConfig(t *testing.T) {
 for _,enabled:=range []bool{true,false} {
  models:=modelsMap([]string{"auto","text-only"},map[string]bool{"auto":enabled,"text-only":false})
  raw,_:=json.Marshal(models["auto"])
  if strings.Contains(string(raw),"image")!=enabled {t.Fatal(string(raw))}
  raw,_=json.Marshal(models["text-only"])
  if strings.Contains(string(raw),"image") {t.Fatal("text model advertised images")}
 }
 raw,_:=json.Marshal(modelsMap([]string{"legacy"}))
 if strings.Contains(string(raw),"capabilities") {t.Fatal("legacy config changed")}
}
`);
add(agent+'runtime/openclaw/vision_test.go',`package openclaw
import("testing"; "encoding/json"; "strings"; "github.com/iamlovingit/clawmanager-agent/internal/gateway")
func TestManagedVisionChangesInputOnly(t *testing.T) {
 for _,enabled:=range []bool{true,false} {
  doc:=map[string]any{}
  mergeOpenClawLLMConfig(doc,gateway.Config{LLMModelIDs:[]string{"auto","text"},LLMVision:map[string]bool{"auto":enabled,"text":false}})
  models:=doc["models"].(map[string]any)["providers"].(map[string]any)["auto"].(map[string]any)["models"].([]any)
  raw,_:=json.Marshal(models[0].(map[string]any)["input"])
  if strings.Contains(string(raw),"image")!=enabled {t.Fatal(string(raw))}
  raw,_=json.Marshal(models[1].(map[string]any)["input"])
  if strings.Contains(string(raw),"image") {t.Fatal(string(raw))}
 }
}
`);
// Legacy standalone OpenClaw has its own configuration manager.
edit('openclaw/internal/configmanager/normalizer.go','\tModelIDs  []string','\tModelIDs  []string\n\tVision map[string]bool');
edit('openclaw/internal/configmanager/normalizer.go','\toverrides.BaseURL = firstNonEmptyEnv("CLAWMANAGER_LLM_BASE_URL")',`\tif raw := strings.TrimSpace(os.Getenv("CLAWMANAGER_LLM_VISION")); raw != "" {
        var values map[string]json.RawMessage
        if err:=json.Unmarshal([]byte(raw),&values);err!=nil||values==nil {return llmOverrides{},false,fmt.Errorf("invalid CLAWMANAGER_LLM_VISION")}
        overrides.Vision=map[string]bool{}
        for id,v:=range values {if string(v)!="true"&&string(v)!="false" {return llmOverrides{},false,fmt.Errorf("invalid vision capability")};overrides.Vision[id]=string(v)=="true"}
    }
\toverrides.BaseURL = firstNonEmptyEnv("CLAWMANAGER_LLM_BASE_URL")`);
edit('openclaw/internal/configmanager/normalizer.go','provider["models"] = buildProviderModels(provider["models"], overrides.ModelIDs)',`provider["models"] = buildProviderModels(provider["models"], overrides.ModelIDs)
        for _,entry:=range provider["models"].([]any) {
            model:=entry.(map[string]any)
            if enabled,ok:=overrides.Vision[stringValue(model["id"])];ok {
                input:=[]any{}
                if previous,ok:=model["input"].([]any);ok {for _,value:=range previous {if s,ok:=value.(string);ok&&!strings.HasPrefix(s,"image") {input=append(input,s)}}}
                if len(input)==0 {input=append(input,"text")}
                if enabled {input=append(input,"image")}
                model["input"]=input
            }
        }`);
add('openclaw/internal/configmanager/vision_test.go',`package configmanager
import("testing"; "encoding/json"; "strings")
func TestVisionEnvironmentAndNormalization(t *testing.T) {
 t.Setenv("CLAWMANAGER_LLM_MODEL",\x60["auto","text"]\x60)
 t.Setenv("CLAWMANAGER_LLM_VISION",\x60{"auto":true,"text":false}\x60)
 overrides,_,err:=readLLMOverridesFromEnv();if err!=nil {t.Fatal(err)}
 doc:=map[string]any{};normalizeLLMConfigContent(doc,overrides)
 models:=doc["models"].(map[string]any)["providers"].(map[string]any)["auto"].(map[string]any)["models"].([]any)
 raw,_:=json.Marshal(models[0].(map[string]any)["input"]);if !strings.Contains(string(raw),"image") {t.Fatal(string(raw))}
 raw,_=json.Marshal(models[1].(map[string]any)["input"]);if strings.Contains(string(raw),"image") {t.Fatal(string(raw))}
 t.Setenv("CLAWMANAGER_LLM_VISION",\x60{"auto":null}\x60)
 if _,_,err:=readLLMOverridesFromEnv();err==nil {t.Fatal("null accepted")}
}
`);
for(const [file,next] of changes) {
 const destination=path.join(root,file).replaceAll('\\','/');
 const exists=fs.existsSync(destination);
 const old=exists?fs.readFileSync(destination,'utf8').replaceAll('\r\n','\n'):'';
 if(old===next)continue;
 let patch;
 if(exists) {
   const a=old.trimEnd().split('\n'),b=next.trimEnd().split('\n');
   let first=0,endA=a.length,endB=b.length;
   while(first<endA && first<endB && a[first]===b[first])first++;
   while(endA>first && endB>first && a[endA-1]===b[endB-1]){endA--;endB--;}
   first=Math.max(0,first-2);endA=Math.min(a.length,endA+2);endB=Math.min(b.length,endB+2);
   patch=`*** Begin Patch\n*** Update File: ${destination}\n@@\n`+a.slice(first,endA).map(x=>'-'+x).join('\n')+'\n'+b.slice(first,endB).map(x=>'+'+x).join('\n')+'\n*** End Patch';
 } else {
   patch=`*** Begin Patch\n*** Add File: ${destination}\n`+next.trimEnd().split('\n').map(x=>'+'+x).join('\n')+'\n*** End Patch';
 }
 try { execFileSync(executable,['--codex-run-as-apply-patch',patch],{stdio:'inherit'}); }
 catch(error){throw new Error(`apply_patch failed for ${file}: ${error.code ?? error.status}`);}
}
console.log('Applied runtime vision source changes only; no deployment performed.');
