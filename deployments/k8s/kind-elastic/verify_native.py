#!/usr/bin/env python3
"""Accept four existing local smoke instances; no model inference is performed."""
import argparse, json, pathlib, re, subprocess, time, urllib.parse
import requests
import urllib3

p = argparse.ArgumentParser()
p.add_argument('--out', type=pathlib.Path, required=True)
p.add_argument('--kubeconfig', required=True)
p.add_argument('--kubectl', default='kubectl')
a = p.parse_args()
credentials = json.loads((a.out/'credentials.json').read_text())
base = credentials['url']
assert urllib.parse.urlparse(base).hostname in ('localhost', '127.0.0.1'), 'Local acceptance only'
urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)
s = requests.Session(); s.verify = False; s.trust_env = False
def api(method, path, body=None):
    r = s.request(method, base+'/api/v1'+path, json=body, timeout=40)
    assert r.ok, (path, r.status_code)
    return r.json()['data']

token = api('POST', '/auth/login', {'username':credentials['username'], 'password':credentials['password']})['access_token']
s.headers['Authorization'] = 'Bearer '+token
kube = [a.kubectl, '--kubeconfig', a.kubeconfig, '-n', 'clawmanager-system']
probe = pathlib.Path(__file__).with_name('probe_isolation.py').read_text()
report = {'passed':False, 'real_llm_inference_tested':False, 'instances':[]}
try:
    deadline = time.monotonic()+300
    previous = None
    while True:
        rows = api('GET', '/instances')['instances']
        rows = [x for x in rows if x['name'].startswith('kind-')]
        state = [(x['id'], x['type'], x['status']) for x in rows]
        if state != previous: print(json.dumps(state), flush=True); previous = state
        assert len(rows)==4, 'Create exactly one kind-* smoke instance of each runtime first'
        if all(x['status']=='running' for x in rows): break
        assert time.monotonic()<deadline, 'Instances did not become running'
        time.sleep(3)
    pools = api('GET', '/admin/runtime-pods')['pods']
    assert len(pools)==4 and all(x['capacity']==8 for x in pools)
    report['pools'] = [{k:x[k] for k in ['runtime_type','capacity','state','used_slots']} for x in pools]
    for instance in sorted(rows, key=lambda x:x['id']):
        identity, kind = instance['id'], instance['type']
        native = requests.Session(); native.verify=False; native.trust_env=False
        descriptor = None
        if kind=='hermes':
            path='/instances/'+str(identity)+'/hermes-desktop/bootstrap'
            descriptor=api('GET',path)
            assert descriptor['available'], ('hermes',descriptor.get('reason'))
            activated=s.post(base+'/api/v1'+path,json={},headers={'Origin':base},timeout=40)
            assert activated.ok and activated.json()['data']['available']
            native.cookies.update(activated.cookies)
            entry=urllib.parse.urljoin(base,descriptor['renderer_url'])
        else:
            access = api('POST', '/instances/'+str(identity)+'/access', {})
            entry = urllib.parse.urljoin(base, access['access_url'])
            for cookie in s.cookies:
                if cookie.name=='instance_access_'+str(identity):
                    native.cookies.set_cookie(cookie)
        parsed = urllib.parse.urlparse(entry)
        assert parsed.hostname in ('localhost','127.0.0.1',kind+'-'+str(identity)+'.127-0-0-1.nip.io')
        # Native origins receive only their instance-scoped cookie, never the
        # management bearer credential used above.
        page = native.get(entry, timeout=60)
        assert page.status_code==200 and 'text/html' in page.headers.get('Content-Type',''), (kind, 'page', page.status_code)
        assets = re.findall(r'(?:src|href)=["\']([^"\']+\.(?:js|css)(?:\?[^"\']*)?)["\']', page.text)
        assert assets, (kind, 'No native assets')
        asset_report = []
        for asset in assets[:2]:
            url = urllib.parse.urljoin(page.url, asset)
            assert urllib.parse.urlparse(url).netloc==parsed.netloc
            r = native.get(url, timeout=40)
            assert r.status_code==200 and ('javascript' in r.headers.get('Content-Type','') or 'css' in r.headers.get('Content-Type','')), (kind, 'asset', r.status_code)
            asset_report.append({'http':r.status_code,'bytes':len(r.content)})
        api('GET', '/instances/'+str(identity)+'/workspace/files')
        origin = parsed.scheme+'://'+parsed.netloc
        native_api = None
        if kind=='hermes':
            r=native.get(base+descriptor['api_base']+'/api/sessions',headers={'Origin':base},timeout=40)
            assert r.status_code==200 and isinstance(r.json(),dict), ('hermes','sessions',r.status_code)
            native_api={'sessions_http':200,'managed_bridge':True}
        elif kind=='opencode':
            r=native.get(origin+'/global/health',timeout=30)
            assert r.status_code==200 and r.json().get('healthy') is True
            native_api={'health_http':200}
        elif kind=='deepseek-harness':
            method='settings/describe'
            r=native.post(origin+'/api/'+method,json={'type':'client-request','rpcId':'kind-readonly','method':method,'payload':{'args':{}}},headers={'Origin':origin},timeout=30)
            assert r.status_code==200 and (r.json().get('result') or {}).get('ok') is True
            native_api={'readonly_rpc_http':200}
        cm=json.loads(subprocess.check_output(kube+['get','cm','lite-instance-'+str(identity),'-o','json']))
        assignment=json.loads(cm['data']['request']); workspace=assignment['workspace_path']
        pool=next(x for x in pools if x['runtime_type']==kind)
        slot=(assignment['gateway_port']-20000)//(1 if kind=='hermes' else 3)
        other=next(x for x in rows if x['id']!=identity)
        hidden='/workspaces/'+other['type']+'/user-1/instance-'+str(other['id'])
        result=subprocess.run(kube+['exec','-i',pool['pod_name'],'-c','slot-'+str(slot),'--','python3','-',str(identity),workspace,hidden],input=probe,text=True,capture_output=True,timeout=30)
        assert result.returncode==0, (kind,'isolation',result.stderr[:300],result.stdout[:500])
        isolation=json.loads(result.stdout); assert isolation['passed']
        report['instances'].append({'id':identity,'type':kind,'status':'running','cpu_cores':instance['cpu_cores'],'native_page_http':200,'assets':asset_report,'workspace_http':200,'native_api':native_api,'isolation':isolation})
    report['passed']=True
finally:
    (a.out/'native-acceptance.json').write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps({'passed':report['passed'],'verified_instances':len(report['instances'])}),flush=True)
