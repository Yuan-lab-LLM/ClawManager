#!/usr/bin/env python3
"""Exercise candidate Lite images in an isolated Kubernetes namespace on the test node."""
import argparse
import base64
import concurrent.futures
import datetime
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import secrets
import ssl
import socket
import subprocess
import threading
import time
import urllib.parse


TYPES = ("openclaw", "hermes", "opencode", "deepseek-harness")
LABEL = "clawmanager.io/slot-test-run"
MOCK = """
import datetime, json, os, threading
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
class Handler(BaseHTTPRequestHandler):
 def log_message(self,*args): pass
 def reply(self,x):
  body=json.dumps(x).encode();self.send_response(200);self.send_header("Content-Type","application/json");self.end_headers();self.wfile.write(body)
 def do_HEAD(self): self.send_response(200);self.end_headers()
 def do_GET(self): self.reply({"mock":True})
 def do_POST(self):
  obj=json.loads(self.rfile.read(int(self.headers.get("Content-Length","0"))) or b"{}")
  if self.path=="/api/v1/runtime/security/lease" and self.headers.get("X-ClawManager-Control-Token")==os.environ["CONTROL"]:
   obj.update(model_token="v1.fixture-model", egress_token="v1.fixture-egress", services_token="v1.fixture-services",expires_at=(datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(minutes=12)).isoformat());self.reply(obj)
  elif self.headers.get("X-ClawManager-Agent-Token")==os.environ["REPORT"] and self.path.startswith("/api/v1/runtime-agent/"):
   if self.path.endswith("/register"):
    print(json.dumps(dict(event="register",**{k:obj.get(k) for k in ("runtime_type","image_digest","pod_uid","pod_name","capabilities")})),flush=True)
   self.reply({"pod_id":1})
  else:self.send_error(403)
ThreadingHTTPServer(("0.0.0.0",9001),Handler).serve_forever()
"""


def run(*argv, data=None, timeout=90):
    result = subprocess.run(argv, input=data, text=True, capture_output=True, timeout=timeout)
    if result.returncode:
        raise RuntimeError(f"{argv[0]} returned {result.returncode}: {result.stderr.strip()[:700]}")
    return result.stdout


def obj(*argv):
    return json.loads(run(*argv, "-o", "json"))


def write(path, data):
    temp = path.with_name(path.name + ".tmp")
    temp.write_text(json.dumps(data, indent=2) + "\n")
    temp.replace(path)


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


class Lab:
    def __init__(self, args):
        self.root = args.output.resolve()
        self.root.mkdir(parents=True, exist_ok=True)
        self.images = json.loads(args.candidates.read_text())
        self.test_injection = args.inject_aux
        assert set(self.images) == set(TYPES)
        self.statepath = self.root / "run.private.json"
        if args.action == "setup":
            if self.statepath.exists():
                raise RuntimeError("This evidence directory already has an owner")
            run_id = secrets.token_hex(6)
            self.state = dict(ns="cm-slot-native-" + run_id, run=run_id, control=secrets.token_hex(32), report=secrets.token_hex(32))
            self.save()
        else:
            self.state = json.loads(self.statepath.read_text())
        self.ns = self.state["ns"]
        if not __debug__: raise RuntimeError("validation requires assertions")
        assert self.ns == "cm-slot-native-" + self.state["run"] and re.fullmatch(r"[a-f0-9]{12}", self.state["run"])
        self.labels = {LABEL:self.state["run"]}

    def save(self):
        write(self.statepath, self.state)

    def get(self, kind, name):
        return obj("kubectl", "-n", self.ns, "get", kind, name)

    def create(self, kind, name, spec, api="v1", annotations=None, **fields):
        body=dict(apiVersion=api, kind=kind, metadata=dict(name=name, labels=self.labels), **fields)
        if annotations:
            body["metadata"]["annotations"]=annotations
        if kind != "Namespace":
            body["metadata"]["namespace"]=self.ns
        if spec is not None:
            body["spec"]=spec
        answer=json.loads(run("kubectl","create","-f","-","-o","json", data=json.dumps(body)))
        if kind=="Namespace":
            self.state["namespace_uid"]=answer["metadata"]["uid"]
            self.save()
        return answer

    def owner(self):
        ns=obj("kubectl","get","namespace",self.ns)
        if ns["metadata"]["uid"]!=self.state["namespace_uid"] or ns["metadata"]["labels"].get(LABEL)!=self.state["run"]:
            raise RuntimeError("namespace ownership changed")

    def service(self,name,selector,ports):
        return self.create("Service",name,{"selector":selector,"ports":[dict(name=k,port=v,targetPort=v) for k,v in ports.items()]})

    def setup(self):
        self.create("Namespace",self.ns,None)
        self.create("PersistentVolumeClaim","workspace",dict(accessModes=["ReadWriteMany"],storageClassName="longhorn-rwx",resources={"requests":{"storage":"1Gi"}}))
        self.create("ServiceAccount","runtime",None,automountServiceAccountToken=False)
        self.create("Secret","credentials",None,stringData={"control":self.state["control"],"report":self.state["report"]})
        cert,key = self.root/"agent.crt", self.root/"agent.key"
        run("openssl","req","-x509","-newkey","rsa:2048","-nodes","-days","2","-subj","/CN=slot-fixture-runtime","-addext","subjectAltName=DNS:lite-agent.clawmanager.internal","-addext","extendedKeyUsage=serverAuth","-keyout",str(key),"-out",str(cert))
        self.create("Secret","clawmanager-lite-agent-tls",None,type="kubernetes.io/tls",data={k:base64.b64encode(p.read_bytes()).decode() for k,p in (("tls.crt",cert),("tls.key",key))})
        self.create("ConfigMap","mock",None,data={"server.py":MOCK})
        cred=lambda k: {"secretKeyRef":{"name":"credentials","key":k}}
        mock=self.create("Pod","mock-cm",{"automountServiceAccountToken":False,"containers":[dict(name="http",image=self.images["deepseek-harness"]["image"],imagePullPolicy="IfNotPresent",command=["python3","/fixture/server.py"],env=[{"name":k.upper(),"valueFrom":cred(k)} for k in ("control","report")],volumeMounts=[dict(name="mock",mountPath="/fixture",readOnly=True)])],"volumes":[dict(name="mock",configMap=dict(name="mock"))]})
        run("kubectl","-n",self.ns,"label","pod","mock-cm","role=mock")
        self.service("mock-cm",{"role":"mock"},{"http":9001})
        for rt in TYPES:
            dep=obj("kubectl","-n","clawmanager-system","get","deployment",rt+"-runtime")
            template=dep["spec"]["template"]["spec"]
            containers=template["containers"][:3]
            assert [c["name"] for c in containers]==["runtime","slot-0","slot-1"]
            for c in containers:
                assert c["image"]==self.images[rt]["base"]
                c["image"]=self.images[rt]["image"]
                c["imagePullPolicy"]="IfNotPresent"
                c["resources"]={"requests":{"cpu":"40m","memory":"96Mi"},"limits":{"cpu":"2","memory":"1536Mi"}}
                env={e["name"]:e for e in c["env"]}
                for k,e in env.items():
                    if "TOKEN" in k:
                        e.clear();e.update(name=k,valueFrom=cred("report" if "REPORT" in k else "control"))
                    elif e.get("valueFrom") and "fieldRef" not in e["valueFrom"]:
                        raise RuntimeError(f"unexpected unowned env reference {k}")
                values={"CLAWMANAGER_BACKEND_URL":"http://mock-cm."+self.ns+".svc.cluster.local:9001","CLAWMANAGER_RUNTIME_IMAGE_REF":c["image"],"CLAWMANAGER_RUNTIME_IMAGE_DIGEST":c["image"].split("@")[1],"RUNTIME_NETWORK_EGRESS_PROXY_URL":"http://mock-cm."+self.ns+".svc.cluster.local:9001"}
                for k in ("RUNTIME_SLOT_COUNT","RUNTIME_NETWORK_SLOT_COUNT"):
                    if k in env: values[k]="2"
                if c["name"]=="runtime":
                    values["RUNTIME_GATEWAY_CAPACITY"]="2"
                    values["RUNTIME_GATEWAY_PORT_END"]="20005"
                for k,v in values.items():env[k]={"name":k,"value":v}
                c["env"]=list(env.values())
            init=template["initContainers"][0]
            init["image"]=self.images[rt]["image"]
            keep={m["name"] for c in containers for m in c["volumeMounts"]}
            keep.add("runtime-agent-tls-source")
            init["volumeMounts"]=[m for m in init["volumeMounts"] if m["name"] in keep]
            paths={m["mountPath"] for m in init["volumeMounts"]}
            lines=init["command"][-1].splitlines()
            init["command"][-1]="\n".join(line for line in lines if all(re.match(r"^/initialize/(?:state-|public-)",v) is None or v in paths for v in line.split()))
            vols=[v for v in template["volumes"] if v["name"] in keep]
            assert all(set(v)-{"name","emptyDir","secret","persistentVolumeClaim"}==set() for v in vols)
            next(v for v in vols if v["name"]=="workspaces")["persistentVolumeClaim"]={"claimName":"workspace"}
            init["volumeMounts"].append({"name":"workspaces","mountPath":"/fixture-share"})
            # Only the new fixture PVC is accessible here. Keep the same manager ownership as NFS production.
            init["command"][-1]+="\nchown 10001:10001 /fixture-share\nchmod 0755 /fixture-share\n"
            spec=dict(automountServiceAccountToken=False, hostUsers=template.get("hostUsers",True), shareProcessNamespace=False,securityContext=template["securityContext"],serviceAccountName="runtime",containers=containers,initContainers=[init],volumes=vols)
            annotations=dep["spec"]["template"]["metadata"].get("annotations",{})
            selected={k:v for k,v in annotations.items() if k in ("container.apparmor.security.beta.kubernetes.io/slot-0","container.apparmor.security.beta.kubernetes.io/slot-1")}
            if self.test_injection and rt in ("opencode","openclaw","deepseek-harness"):
                instrument=json.loads(self.test_injection.read_text())
                image=instrument["image"]
                if not re.fullmatch(r"clawmanager.local/runtime-slot-acceptance-instrumentation@sha256:[a-f0-9]{64}",image):
                    raise ValueError("invalid test-only instrumentation image")
                digest=instrument["binaries"][rt]
                assert re.fullmatch("[a-f0-9]{64}",digest)
                instrument_path="/opt/slot-fixture/"+rt
                command=f"echo '{digest}  {instrument_path}' | sha256sum -c - && cp {instrument_path} /fixture/worker && chmod 0555 /fixture/worker"
                spec["initContainers"].insert(0,{"name":"test-aux-io-injection","image":image,"imagePullPolicy":"IfNotPresent",
                    "command":["/bin/sh","-ec",command],"volumeMounts":[{"name":"test-injection","mountPath":"/fixture"}]})
                spec["volumes"].append({"name":"test-injection","emptyDir":{}})
                slot=spec["containers"][1]
                assert slot["name"]=="slot-0"
                slot["env"].append({"name":"CM_SLOT_FIXTURE_AUX_IO_ERROR","value":"1"})
                slot["volumeMounts"].append({"name":"test-injection","mountPath":"/usr/local/bin/clawmanager-agent","subPath":"worker","readOnly":True})
                self.state["instrumentation"]=instrument
                self.save()
            self.create("Pod",rt,spec,annotations=selected)
            run("kubectl","-n",self.ns,"label","pod",rt,"runtime="+rt)
            self.service(rt,{"runtime":rt},{"agent":19090,"target":24100,"neighbor":24101,"gateway-0":20000,"gateway-1":20000+int(next(e["value"] for e in containers[0]["env"] if e["name"]=="RUNTIME_GATEWAY_PORT_BLOCK_SIZE"))})
        print(self.ns,flush=True)

    def readiness(self):
        self.owner()
        self.state["pods"]={}
        for rt in TYPES:
            run("kubectl","-n",self.ns,"wait","--for=condition=Ready","pod/"+rt,"--timeout=150s",timeout=155)
            p=self.get("pod",rt)
            self.state["pods"][rt]={"uid":p["metadata"]["uid"],"ip":p["status"]["podIP"],"service":self.get("svc",rt)["spec"]["clusterIP"],"node":p["spec"]["nodeName"],"containers":{c["name"]:c["containerID"] for c in p["status"]["containerStatuses"]}}
            self.save()
        rows=[]
        for line in run("kubectl","-n",self.ns,"logs","mock-cm").splitlines():
            if line.startswith("{"): rows.append(json.loads(line))
        checks={}
        for rt in TYPES:
            p=self.state["pods"][rt]
            reg=next(x for x in reversed(rows) if x["pod_uid"]==p["uid"])
            assert reg["image_digest"]==self.images[rt]["image"].split("@")[1] and "manual-slot-reset-v1" in reg["capabilities"]
            st=self.api(rt,"GET","/v1/slots/slot-0/state")
            assert st["idle_confirmed"] and st["pod_uid"]==p["uid"]
            fs=run("kubectl","-n",self.ns,"exec",rt,"-c","slot-0","--","stat","-f","-c","%T","/workspaces").strip()
            assert fs=="nfs"
            checks[rt]=dict(registration=reg,slot=st,filesystem=fs)
        write(self.root/"registration.json",checks)

    def request(self, rt, method, path, payload=None, headers=None, timeout=40, address="ip"):
        ip = self.state["pods"][rt][address]
        ctx = ssl.create_default_context(cafile=str(self.root / "agent.crt"))
        conn = http.client.HTTPSConnection("lite-agent.clawmanager.internal", 19090, timeout=timeout, context=ctx)
        conn._create_connection = lambda address, *a, **kw: socket.create_connection((ip, address[1]), *a, **kw)
        credentials = {"X-ClawManager-Control-Token": self.state["control"], "Content-Type": "application/json"}
        try:
            conn.request(method, path, body=json.dumps(payload) if payload is not None else None,
                         headers={**credentials, **(headers or {})})
            reply = conn.getresponse()
            return reply.status, reply.read(65536)
        finally:
            conn.close()

    def api(self, rt, method, path, payload=None):
        status, raw = self.request(rt, method, path, payload)
        if status >= 300:
            raise RuntimeError(f"{method} {path} returned {status}: {raw[:230].decode(errors='replace')}")
        return json.loads(raw)

    def scope(self, rt, slot):
        instance = 100 + 10 * TYPES.index(rt) + slot
        return {"instance_id": instance, "user_id": 1, "generation": 1, "gateway_id": f"gw-{instance}-1"}

    def preview(self, rt, slot, timeout=3, address="ip"):
        route = {**self.scope(rt, slot), "scheme":"http", "host":"127.0.0.1", "port":24100+slot, "path":"/index.json"}
        encoded = base64.urlsafe_b64encode(json.dumps(route).encode()).rstrip(b"=").decode()
        status, raw = self.request(rt, "GET", "/v1/workbench/preview",
                                   headers={"X-ClawManager-Workbench-Preview": encoded}, timeout=timeout, address=address)
        if status != 200:
            raise RuntimeError(f"preview HTTP {status}")
        return json.loads(raw)

    def snapshot(self, rt, slot):
        return self.api(rt, "GET", f"/v1/slots/slot-{slot}/state")

    def sites(self):
        self.owner()
        def launch(args):
            rt, slot = args
            scope = self.scope(rt, slot)
            def wb(op, data):
                return self.api(rt, "POST", "/v1/workbench", {**scope, "operation":op, "data":data})
            record = {"runtime":rt, "slot":slot, "scope":scope}
            try:
                snapshot = self.snapshot(rt, slot)
                record["before"] = snapshot
                for _ in range(90):
                    if any(g.get("state")=="running" for g in snapshot["gateways"]):
                        break
                    if any(g.get("state")=="error" for g in snapshot["gateways"]):
                        break
                    time.sleep(.5)
                    snapshot=self.snapshot(rt,slot)
                session = wb("terminal.create", {"cols":100, "rows":24})["id"]
                record["session"] = session
                name = f"slot-fixture-{self.state['run']}"
                code = "\n".join([
                    "import http.server,json,os,pathlib",
                    f"directory=pathlib.Path.home()/{name!r}",
                    "directory.mkdir(exist_ok=True)",
                    "os.chdir(directory)",
                    f"marker=dict(instance={scope['instance_id']},run={self.state['run']!r},port={24100+slot},uid=os.geteuid(),gid=os.getegid(),pid=os.getpid(),pidns=os.readlink('/proc/self/ns/pid'),netns=os.readlink('/proc/self/ns/net'),home=str(directory))",
                    "pathlib.Path('index.json').write_text(json.dumps(marker))",
                    f"http.server.ThreadingHTTPServer(('127.0.0.1',{24100+slot}),http.server.SimpleHTTPRequestHandler).serve_forever()"])
                command = "exec python3 -c \"exec(__import__('base64').b64decode('" + base64.b64encode(code.encode()).decode() + "'))\"\n"
                wb("terminal.write", {"session":session,"input":command})
                for _ in range(12):
                    try:
                        record["site"] = self.preview(rt, slot)
                        break
                    except Exception as ex:
                        record["last_preview_error"] = str(ex)
                        time.sleep(.5)
                if "site" not in record:
                    raise RuntimeError("no validated preview response")
                assert record["site"]["uid"] == 200000 + scope["instance_id"]
            except Exception as ex:
                record["error"] = str(ex)
            return f"{rt}-{slot}", record
        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as workers:
            results=dict(workers.map(launch,[(rt, slot) for rt in TYPES for slot in range(2)]))
        write(self.root/"sites.json",results)
        print({k:v.get("error","HTTP 200") for k,v in results.items()})

    def assign(self):
        self.owner()
        mock_ip=self.get("service","mock-cm")["spec"]["clusterIP"]
        def create(rt):
            reports=[]
            for slot in range(2):
                instance=100+10*TYPES.index(rt)+slot
                port=next(x["port"] for x in self.get("service",rt)["spec"]["ports"] if x["name"]=="gateway-"+str(slot))
                token="ui1_"+hashlib.sha256((self.state["control"]+rt+str(slot)).encode()).hexdigest()
                pod=self.state["pods"][rt]
                payload={"instance_id":instance,"user_id":1,"generation":1,"agent_type":rt,"gateway_port":port,"uid":200000+instance,"gid":200000+instance,"pod_uid":pod["uid"],"workspace_path":f"/workspaces/{rt}/user-1/instance-{instance}","security_profile":"isolated-network-v1","cpu_cores":1,"disk_quota_mb":64,"memory_mb":384,"environment":{"CLAWMANAGER_INSTANCE_TOKEN":token,"CLAWMANAGER_LLM_BASE_URL":"http://clawmanager-local-model.invalid:31281/api/v1/gateway/llm","CLAWMANAGER_LLM_API_KEY":"cm-local-model","CLAWMANAGER_LLM_MODEL":"auto","CLAWMANAGER_MODEL_GOVERNANCE_PROTOCOL":"1","CLAWMANAGER_GATEWAY_GENERATION":"1"}}
                try:
                    policy=self.api(rt,"GET","/v1/proxy-policy")
                    self.api(rt,"PUT","/v1/proxy-policy",{"namespace":self.ns,"proxies":[mock_ip],"expected_revision":policy.get("revision","")})
                except RuntimeError as e:
                    if "returned 404" not in str(e): reports.append({"policy":str(e)})
                try:
                    reply=self.api(rt,"POST","/v1/gateways",payload)
                except RuntimeError as e:
                    reply={"error":str(e)}
                reports.append({"slot":slot,"instance_id":instance,"response":reply})
            return rt,reports
        with concurrent.futures.ThreadPoolExecutor(max_workers=4) as workers:
            replies=dict(workers.map(create,TYPES))
        write(self.root/"assign.json",replies)
        print(json.dumps(replies))

    def pod(self, rt):
        p = self.get("pod", rt)
        assert p["metadata"]["uid"] == self.state["pods"][rt]["uid"]
        assert p["metadata"]["labels"].get(LABEL) == self.state["run"]
        assert all(c["image"] == self.images[rt]["image"] for c in p["spec"]["containers"])
        return p

    def containers(self, rt):
        return {c["name"]:c for c in self.pod(rt)["status"]["containerStatuses"]}

    def marker(self, rt, slot, site):
        expected = f"/workspaces/{rt}/user-1/instance-{self.scope(rt,slot)['instance_id']}/home/slot-fixture-{self.state['run']}"
        assert site["home"] in (expected, expected.replace("/home/slot-fixture-", "/slot-fixture-"))
        expected = site["home"]
        return run("kubectl","-n",self.ns,"exec",rt,"-c",f"slot-{slot}","--","sha256sum",expected+"/index.json").split()[0]

    def ingress(self, rt, slot, address):
        port = self.port(rt, slot)
        token = "ui1_" + hashlib.sha256((self.state["control"]+rt+str(slot)).encode()).hexdigest()
        base=f"http://{address}:{port}"
        path="/"
        statuses=[]
        for _ in range(4):
            conn=http.client.HTTPConnection(address,port,timeout=1)
            try:
                conn.request("GET",path,headers={"X-ClawManager-Ingress-Token":token})
                res=conn.getresponse()
                statuses.append(res.status)
                location=res.getheader("Location","")
                res.read(16384)
            finally:
                conn.close()
            if res.status not in (301,302,303,307,308) or not location:
                break
            target=urllib.parse.urlsplit(urllib.parse.urljoin(base+path,location))
            if (target.scheme,target.netloc)!=("http",f"{address}:{port}"):
                break
            path=urllib.parse.urlunsplit(("", "", target.path or "/", target.query,""))
        return statuses

    def port(self, rt, slot):
        return 20000 + (3 if rt in ("opencode","openclaw") else 1)*slot

    def monitor(self, rt, slot, event, samples):
        while not event.is_set():
            row = {"time":now()}
            for label in ("ip","service"):
                try:
                    row[label] = self.ingress(rt,slot,self.state["pods"][rt][label])
                except Exception as ex:
                    row[label] = str(ex)
            try:
                site=self.preview(rt,slot,timeout=1)
                row["preview"] = {"pid":site["pid"], "pidns":site["pidns"], "uid":site["uid"]}
            except Exception as ex:
                row["preview"] = str(ex)
            try:
                svc=self.preview(rt,slot,timeout=1,address="service")
                row["service_preview"]={"pid":svc["pid"],"uid":svc["uid"]}
            except Exception as ex:
                row["service_preview"]=str(ex)
            samples.append(row)
            event.wait(.15)

    def endpoints(self,rt):
        rows=obj("kubectl","-n",self.ns,"get","endpointslices","-l","kubernetes.io/service-name="+rt)
        return [{"addresses":e["addresses"],"conditions":e["conditions"],"uid":e["targetRef"]["uid"]}
                for s in rows["items"] for e in s.get("endpoints",[])]

    def helper(self, rt, slot, old_id, log):
        node = obj("kubectl","get","node",self.state["pods"][rt]["node"])
        p = self.pod(rt)
        name = f"slot-{slot}"
        op = "fixture-"+secrets.token_hex(12)
        scope = dict(version=1,operation_id=op,namespace=self.ns,pod_name=rt,pod_uid=p["metadata"]["uid"],
                     node_name=node["metadata"]["name"],node_uid=node["metadata"]["uid"],node_boot_id=node["status"]["nodeInfo"]["bootID"],
                     container_name=name,container_id=old_id)
        assert self.containers(rt)[name]["containerID"]==old_id and p["spec"]["nodeName"]==scope["node_name"]
        helper = self.root.parent/"helper"
        ref = json.loads((helper/"candidate.json").read_text())["image"]
        job = json.loads(run(str(helper/"fixture-job"),ref,data=json.dumps(scope)))
        spec = job["spec"]["template"]["spec"]
        assert job["metadata"]["namespace"]==self.ns and spec["nodeName"]==scope["node_name"]
        assert len(spec["containers"])==1 and spec["containers"][0]["image"]==ref
        assert json.loads(spec["containers"][0]["env"][0]["value"]) == scope
        job["metadata"]["labels"].update(self.labels)
        job["spec"]["template"]["metadata"]["labels"].update(self.labels)
        result=json.loads(run("kubectl","create","-f","-","-o","json",data=json.dumps(job)))
        log["helper"] = {"scope":scope, "job":result["metadata"]["name"], "job_uid":result["metadata"]["uid"], "image":ref}
        write(self.root/f"{op}-job.json",log["helper"])
        for _ in range(90):
            pods=obj("kubectl","-n",self.ns,"get","pod","-l","job-name="+result["metadata"]["name"])["items"]
            if len(pods)==1 and pods[0]["status"]["phase"] in ("Succeeded","Failed"):
                pod=pods[0]
                assert pod["metadata"]["labels"].get(LABEL)==self.state["run"]
                assert any(x["uid"]==result["metadata"]["uid"] and x["controller"] for x in pod["metadata"]["ownerReferences"])
                term=pod["status"]["containerStatuses"][0]["state"]["terminated"]
                log["helper"]["pod_uid"]=pod["metadata"]["uid"]
                if term["exitCode"]:
                    raise RuntimeError("physical helper failed: "+run("kubectl","-n",self.ns,"logs",pod["metadata"]["name"])[:400])
                receipt=json.loads(term["message"])
                digest=hashlib.sha256(json.dumps(scope,separators=(",",":")).encode()).hexdigest()
                assert receipt["confirmed"] and receipt["scope_digest"]==digest
                assert all(receipt[k]==scope[k] for k in ("operation_id","container_id","container_name","pod_uid","node_uid","node_boot_id"))
                log["helper"]["receipt"]=receipt
                return {"scope":scope, "receipt":receipt}
            time.sleep(.5)
        raise RuntimeError("helper deadline exceeded")

    def watch_kubelet(self,rt,slot,event,log):
        while not event.is_set():
            try:
                c=self.containers(rt)[f"slot-{slot}"]
                ep=self.endpoints(rt)
                row=dict(id=c.get("containerID"),restart_count=c.get("restartCount"),state=c.get("state"),endpoints=ep)
                if not log or log[-1]["observation"]!=row:
                    log.append({"time":now(),"observation":row})
            except RuntimeError as ex:
                log.append({"time":now(),"observation":{"error":str(ex)}})
            event.wait(.2)

    def scenario(self, rt, slot, kind, name=None, devtools=False):
        self.owner()
        path = self.root / ((name or f"{rt}-{slot}-{kind}") + ".json")
        if path.exists():
            raise RuntimeError("evidence file exists, select a fresh scenario")
        sites=json.loads((self.root/"sites.json").read_text())
        site=sites[f"{rt}-{slot}"].get("site")
        old=self.snapshot(rt,slot)
        assert old["pod_uid"]==self.state["pods"][rt]["uid"]
        before=self.containers(rt)
        key=f"slot-{slot}"
        cid=before[key]["containerID"]
        log={"namespace":self.ns,"namespace_uid":self.state["namespace_uid"],"scenario":kind,"start":now(),
             "before":old,"sites":{k:v.get("site") for k,v in sites.items() if k.startswith(rt+"-")},
             "before_containers":before,"endpoint_before":self.endpoints(rt)}
        event=threading.Event()
        samples=[]
        thread=threading.Thread(target=self.monitor,args=(rt,1-slot,event,samples))
        thread.start()
        klog=[]
        poller=threading.Thread(target=self.watch_kubelet,args=(rt,slot,event,klog))
        poller.start()
        try:
            if kind!="manual":
                log["background_webserver"]=self.background(rt,slot,devtools)
            if site:
                log["marker_before"]=self.marker(rt,slot,site)
            if kind=="manual":
                proof=self.helper(rt,slot,cid,log)
            else:
                # A normal joined terminal is part of the real primary's private network.
                session=sites[f"{rt}-{slot}"].get("session")
                if session:
                    try:
                        log["terminal_close"]=self.api(rt,"POST","/v1/workbench",{**self.scope(rt,slot),"operation":"terminal.close","data":{"session":session}})
                    except Exception as ex:
                        log["terminal_close_error"]=str(ex)
                scope=self.scope(rt,slot)
                intent=dict(version=1,operation_id=secrets.token_hex(16),pod_uid=old["pod_uid"],slot=key,
                            gateway_id=scope["gateway_id"],instance_id=scope["instance_id"],user_id=1,generation=1,
                            worker_epoch=old["worker_epoch"],assignment_digest=old["digest"],graceful=kind=="sleep")
                if kind=="force-delete":
                    intent["kind"]=kind
                log["intent"]=intent
                method="POST" if kind=="sleep" else "DELETE"
                log["accepted"]=self.api(rt,method,"/v1/gateways/"+scope["gateway_id"]+("/stop" if kind=="sleep" else ""),intent)
                assert log["accepted"]["pending"] and not log["accepted"]["confirmed"]
            seen=[]
            for _ in range(140):
                c=self.containers(rt)[key]
                t=c.get("lastState",{}).get("terminated",{})
                row=[now(),c.get("containerID"),c.get("restartCount"),list(c["state"].keys())[0],c["state"].get("waiting",{}).get("reason")]
                if not seen or row[1:]!=seen[-1][1:]:
                    seen.append(row)
                if c.get("containerID")!=cid and c["state"].get("running") and t.get("containerID")==cid:
                    break
                time.sleep(.5)
            else:
                raise RuntimeError("kubelet did not report pinned old termination")
            log["kubelet_transitions"]=seen
            log["terminated"]=t
            if kind == "manual":
                for _ in range(35):
                    try:
                        current=self.snapshot(rt,slot)
                        if current["worker_epoch"]!=old["worker_epoch"]: break
                    except Exception:
                        pass
                    time.sleep(.4)
                assert current["worker_epoch"]!=old["worker_epoch"]
                proof.update(worker_epoch=(old.get("assignment") or {}).get("worker_epoch",old.get("worker_epoch","")), assignment_digest=old.get("digest",""), current_worker_epoch=current["worker_epoch"])
                suffix="reset"
            else:
                msg=t.get("message","")
                maybe=msg.startswith("C2:")
                fields={**intent,"cleanup_confirmed":not maybe}
                if maybe:fields["auxiliary_exit_pending"]=True
                digest=hashlib.sha256(json.dumps(fields,separators=(",",":")).encode()).digest()
                assert msg==("C2:" if maybe else "C1:")+base64.urlsafe_b64encode(digest).rstrip(b"=").decode()
                proof={**fields,"container_id":cid,"finished_at":t["finishedAt"]}
                suffix="stop"
            for op in ("reconcile","release"):
                log[op]=self.api(rt,"POST",f"/v1/slots/{key}/{op}-{suffix}",proof)
                assert log[op]["confirmed" if op=="reconcile" else "released"]
            log["after"]=self.snapshot(rt,slot)
            log["after_containers"]=self.containers(rt)
            assert all(v["containerID"]==log["after_containers"][k]["containerID"] for k,v in before.items() if k!=key)
            if site:
                try:
                    log["target_preview_after"]=self.preview(rt,slot)
                    raise ValueError("target website still routed")
                except (RuntimeError,OSError) as ex:
                    log["target_preview_after_error"]=str(ex)
                log["marker_after"]=self.marker(rt,slot,site)
                assert log["marker_before"]==log["marker_after"]
            time.sleep(1)
        except Exception as ex:
            log["error"]=f"{type(ex).__name__}: {ex}"
        finally:
            event.set()
            thread.join(8)
            poller.join(8)
            log["kubelet_and_endpoints"]=klog
            log["samples"]=samples
            log["endpoint_after"]=self.endpoints(rt)
            log["finished"]=now()
            write(path,log)
        return {k:log.get(k) for k in ("scenario","error","terminated","release")}

    def background(self, rt, slot, devtools=False):
        scope=self.scope(rt,slot)
        payload={**scope, "operation":"terminal.create","data":{"cols":80,"rows":24}}
        session=self.api(rt,"POST","/v1/workbench",payload)["id"]
        # User-style background web service launched independently of the interactive shell.
        command=f"nohup python3 -m http.server {25100+slot} --bind 127.0.0.1 > /dev/null 2>&1 < /dev/null &\n"
        if devtools:
            # Start a normal, empty SSH agent as part of a developer terminal session.
            command="ssh-agent -s >/dev/null\n"+command
        payload.update(operation="terminal.write",data={"session":session,"input":command})
        self.api(rt,"POST","/v1/workbench",payload)
        time.sleep(1)
        payload.update(operation="terminal.close",data={"session":session})
        try:
            return self.api(rt,"POST","/v1/workbench",payload)
        except RuntimeError as ex:
            return {"error":str(ex)}

    def more(self):
        self.owner()
        tasks=[("opencode",1,"stop"),("deepseek-harness",1,"force-delete"),("hermes",1,"sleep")]
        def test(args):
            rt,slot,kind=args
            return self.scenario(rt,slot,kind,name=f"{rt}-{slot}-{kind}-background",devtools=True)
        with concurrent.futures.ThreadPoolExecutor(max_workers=len(tasks)) as pool:
            for reply in pool.map(test,tasks): print(reply,flush=True)

    def migrated(self):
        self.owner()
        rt="hermes"
        destination=self.pod(rt)
        expected=self.preview(rt,1)
        target=self.scope(rt,1)
        site_dir=f"/workspaces/.clawmanager-slot-locks/hermes-1-{target['instance_id']}"
        def writer():
            code="""import os,sys,json,hashlib,fcntl
os.setgroups([]);os.setgid(10001);os.setuid(10001)
prefix=sys.argv[1];data=open(prefix+'.json','rb').read()
with open(prefix+'.lock','r+b') as f:
 try:fcntl.flock(f,fcntl.LOCK_EX|fcntl.LOCK_NB)
 except BlockingIOError: held=True
 else:held=False;fcntl.flock(f,fcntl.LOCK_UN)
print(json.dumps(dict(record=json.loads(data),sha256=hashlib.sha256(data).hexdigest(),held=held)))"""
            return json.loads(run("kubectl","-n",self.ns,"exec",rt,"-c","slot-1","--","python3","-c",code,site_dir))
        historical="hermes-history"
        if historical in self.state["pods"]:
            raise RuntimeError("historical fixture already exists")
        pre=writer()
        assert pre["held"] and pre["record"]["pod_uid"]==destination["metadata"]["uid"] and pre["record"]["slot"]=="slot-1"
        spec=destination["spec"]
        init=spec["initContainers"][0]
        state_mount=next(m["mountPath"] for m in init["volumeMounts"] if m["name"]=="slot-0-state")
        init["env"]=[*init.get("env",[]),{"name":"HISTORY_POD_UID","valueFrom":{"fieldRef":{"fieldPath":"metadata.uid"}}}]
        # A new emptyDir represents a historical source book. Do not modify the real successor's NFS record.
        init["command"][-1]="\n".join(l for l in init["command"][-1].splitlines() if "/fixture-share" not in l)
        create_book="""import os,json,hashlib,sys
p=sys.argv[1];scope=json.loads(sys.argv[2])
req={**scope,'pod_uid':os.environ['HISTORY_POD_UID'],'worker_epoch':sys.argv[3],'state':'starting'}
raw=json.dumps(req,separators=(',',':')).encode()
with open(p,'x') as f:json.dump(dict(assignment=req,digest=hashlib.sha256(raw).hexdigest()),f)
os.chown(p,10001,10001);os.chmod(p,0o600)"""
        import shlex
        init["command"][-1]+="\n"+shlex.join(["python3","-c",create_book,state_mount+"/slot-assignment.json",json.dumps(target),secrets.token_hex(16)])
        self.images[historical]=self.images[rt]
        hist=self.create("Pod",historical,spec,annotations=destination["metadata"].get("annotations"))
        run("kubectl","-n",self.ns,"wait","--for=condition=Ready","pod/"+historical,"--timeout=90s",timeout=95)
        hp=self.get("pod",historical)
        self.state["pods"][historical]={"uid":hist["metadata"]["uid"],"ip":hp["status"]["podIP"],"node":hp["spec"]["nodeName"]}
        self.save()
        old=self.snapshot(historical,0)
        assert old["quarantined"] and old["assignment"]["gateway_id"]==target["gateway_id"] and old["assignment"]["pod_uid"]==hp["metadata"]["uid"]
        cid=self.containers(historical)["slot-0"]["containerID"]
        record={"namespace":self.ns,"source":old,"destination":self.snapshot(rt,1),"history":"constructed from fixture identity in new source emptyDir before startup","before_writer":pre,"before_http":expected}
        ev=threading.Event()
        rows=[]
        thread=threading.Thread(target=self.monitor,args=(rt,1,ev,rows))
        thread.start()
        try:
            proof=self.helper(historical,0,cid,record)
            for _ in range(80):
                try:
                    current=self.snapshot(historical,0)
                    c=self.containers(historical)["slot-0"]
                    if current["worker_epoch"]!=old["worker_epoch"] and c.get("lastState",{}).get("terminated",{}).get("containerID")==cid:
                        break
                except (RuntimeError,OSError):
                    pass
                time.sleep(.25)
            else:raise RuntimeError("no old historical CID exit")
            proof.update(worker_epoch=old["assignment"]["worker_epoch"],assignment_digest=old["digest"],current_worker_epoch=current["worker_epoch"])
            record["reconcile"]=self.api(historical,"POST","/v1/slots/slot-0/reconcile-reset",proof)
            record["release"]=self.api(historical,"POST","/v1/slots/slot-0/release-reset",proof)
            record["replay"]=self.api(historical,"POST","/v1/slots/slot-0/release-reset",proof)
            assert record["reconcile"]["confirmed"] and record["release"]["released"] and record["replay"]["released"]
            record["after_writer"]=writer()
            record["after_http"]=self.preview(rt,1)
            assert record["after_writer"]==pre and record["after_http"]==expected
            record["stopped_book"]=self.snapshot(historical,0)
        except Exception as ex:
            record["error"]=repr(ex)
        finally:
            ev.set()
            thread.join(8)
            record["samples"]=rows
            write(self.root/"historical-same-generation.json",record)
        print(record.get("error","historical source released; live writer and HTTP unchanged"))

    def backoff(self):
        print(self.scenario("hermes",0,"manual","hermes-repeated-physical"),flush=True)

    def exercise(self):
        self.owner()
        tasks=[("hermes",0,"manual"),("deepseek-harness",0,"sleep"),("opencode",0,"stop"),("openclaw",0,"force-delete")]
        if "instrumentation" in self.state:
            tasks=[t for t in tasks if t[2]!="manual"]
        with concurrent.futures.ThreadPoolExecutor(max_workers=len(tasks)) as workers:
            for reply in workers.map(lambda x:self.scenario(*x),tasks):
                print(reply,flush=True)

    def cleanup(self):
        self.owner()
        options={"apiVersion":"v1","kind":"DeleteOptions","preconditions":{"uid":self.state["namespace_uid"]}}
        run("kubectl","delete","--raw","/api/v1/namespaces/"+self.ns,"-f","-",data=json.dumps(options))
        run("kubectl","wait","--for=delete","namespace/"+self.ns,"--timeout=180s",timeout=190)
        write(self.root/"cleanup.json",{"namespace":self.ns,"uid":self.state["namespace_uid"],"deleted_at":now()})
        print("Deleted owned namespace",self.ns)

def main():
    parser=argparse.ArgumentParser()
    parser.add_argument("action",choices=["setup","readiness","assign","sites","exercise","more","backoff","migrated","cleanup"])
    parser.add_argument("--output",type=Path,required=True)
    parser.add_argument("--candidates",type=Path,required=True)
    parser.add_argument("--inject-aux",type=Path,help="Explicit test-only I/O fault manifest for new fixtures")
    args=parser.parse_args()
    os.umask(0o077)
    lab=Lab(args)
    getattr(lab,args.action)()

if __name__=="__main__": main()
