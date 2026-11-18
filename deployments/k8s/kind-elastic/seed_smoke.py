#!/usr/bin/env python3
"""Create local startup fixtures with a dummy model; never call inference."""
import argparse, json, pathlib, urllib.parse
import requests
import urllib3

p = argparse.ArgumentParser(); p.add_argument('--out', type=pathlib.Path, required=True); a=p.parse_args()
c=json.loads((a.out/'credentials.json').read_text()); base=c['url']
assert urllib.parse.urlparse(base).hostname in ('localhost','127.0.0.1'), 'Local fixtures only'
urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)
s=requests.Session(); s.verify=False; s.trust_env=False
def api(method,path,body=None):
    r=s.request(method,base+'/api/v1'+path,json=body,timeout=60)
    assert r.ok,(path,r.status_code)
    return r.json()['data']
s.headers['Authorization']='Bearer '+api('POST','/auth/login',{'username':c['username'],'password':c['password']})['access_token']
models=api('GET','/admin/models')['items']
if not models:
    api('PUT','/admin/models',{'display_name':'kind-smoke-local','description':'Startup fixture only; configure a real provider before conversations','provider_type':'openai','protocol_type':'openai','base_url':'http://127.0.0.1:9/v1','provider_model_name':'kind-smoke','api_key':'local-smoke-dummy-key','is_active':True,'is_secure':False})
instances=api('GET','/instances')['instances']
for kind in ['openclaw','hermes','opencode','deepseek-harness']:
    if any(x['name']=='kind-'+kind for x in instances): continue
    row=api('POST','/instances',{'name':'kind-'+kind,'type':kind,'mode':'lite','instance_mode':'lite','runtime_type':'gateway','cpu_cores':0.5,'memory_gb':1,'disk_gb':5,'os_type':'linux','os_version':'slim'})
    print(json.dumps({'id':row['id'],'name':row['name'],'status':row['status']}),flush=True)
