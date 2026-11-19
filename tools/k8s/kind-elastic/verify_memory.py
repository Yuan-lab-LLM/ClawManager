#!/usr/bin/env python3
"""Live, scaled tests of Pod budgets. Temporary Pods never mount user data."""
import argparse,datetime,json,pathlib,subprocess,time
p=argparse.ArgumentParser();p.add_argument('--kubeconfig',required=True);p.add_argument('--kubectl',default='kubectl');p.add_argument('--image',required=True);p.add_argument('--out',type=pathlib.Path,required=True);p.add_argument('--docker-context',default='colima-cm-kind');p.add_argument('--node',default='cm-elastic-control-plane');a=p.parse_args();a.out.mkdir(parents=True,exist_ok=True)
ns='cm-memory-tests';prefix=[a.kubectl,'--kubeconfig',a.kubeconfig,'-n',ns];docker=['docker','--context',a.docker_context,'exec',a.node]
def run(args,body=None,timeout=60):
 q=subprocess.run(args,input=body,text=True,capture_output=True,timeout=timeout)
 if q.returncode:raise RuntimeError(q.stderr[:600])
 return q.stdout
def k(*args,**kw):return run(prefix+list(args),**kw)
def read_group(path):
 files=['memory.max','memory.current','memory.min','memory.low','memory.high','memory.events.local']
 lines=run(docker+['cat',*[path+'/'+f for f in files]]).splitlines();d=dict(zip(files[:5],lines[:5]));d['events']=dict((line.split()[0],int(line.split()[1])) for line in lines[5:]);return d
script='''import json,os,time
mb=int(os.environ['TARGET_MB']);time.sleep(5);chunks=[]
for _ in range(mb):
 chunks.append(bytearray(b'x'*1048576));time.sleep(.01)
print(json.dumps({'allocated_mb':mb}),flush=True)
while True:time.sleep(1)
'''
report={'passed':False,'scale':32,'formal_pod_memory':'16Gi','scaled_pod_memory':'512Mi','slot_request':'32Mi','slot_limit':'128Mi','no_user_data':True,'cases':[]}
created=[]
try:
 namespace={'apiVersion':'v1','kind':'Namespace','metadata':{'name':ns}}
 run([a.kubectl,'--kubeconfig',a.kubeconfig,'apply','-f','-'],json.dumps(namespace))
 for mode in ['borrow','individual','aggregate']:
  name='cm-memory-'+mode+'-'+datetime.datetime.now().strftime('%H%M%S')
  containers=[]
  for i in range(8):
   target=96 if mode=='aggregate' or (mode=='borrow' and i==0) else (160 if mode=='individual' and i==0 else 32)
   containers.append({'name':'slot-'+str(i),'image':a.image,'command':['/usr/bin/tini','--','python3','-u','-c',script],'env':[{'name':'TARGET_MB','value':str(target)}],
    'resources':{'requests':{'cpu':'1m','memory':'32Mi'},'limits':{'cpu':'100m','memory':'128Mi'}},'securityContext':{'runAsUser':200001+i,'runAsGroup':200001+i,'runAsNonRoot':True,'allowPrivilegeEscalation':False,'capabilities':{'drop':['ALL']},'seccompProfile':{'type':'RuntimeDefault'}}})
  containers.append({'name':'coordinator','image':a.image,'command':['/bin/sleep','3600'],'resources':{'requests':{'cpu':'2m','memory':'16Mi'},'limits':{'cpu':'100m','memory':'16Mi'}},'securityContext':{'runAsUser':1000,'runAsNonRoot':True,'allowPrivilegeEscalation':False,'capabilities':{'drop':['ALL']}}})
  manifest={'apiVersion':'v1','kind':'Pod','metadata':{'name':name,'namespace':ns},'spec':{'hostUsers':False,'automountServiceAccountToken':False,'restartPolicy':'Never','terminationGracePeriodSeconds':2,'resources':{'requests':{'cpu':'25m','memory':'512Mi'},'limits':{'cpu':'1','memory':'512Mi'}},'containers':containers}}
  k('create','-f','-',body=json.dumps(manifest));created.append(name)
  deadline=time.monotonic()+100
  while True:
   pod=json.loads(k('get','pod',name,'-o','json'));statuses=pod.get('status',{}).get('containerStatuses',[])
   assert time.monotonic()<deadline,mode+' did not reach expected state: '+json.dumps(pod.get('status',{}))[:600]
   failures=[c for c in statuses if c.get('state',{}).get('waiting',{}).get('reason') in ('CreateContainerError','CreateContainerConfigError','ErrImagePull','ImagePullBackOff')]
   assert not failures,[(c['name'],c['state']) for c in failures]
   unexpected=[c for c in statuses if c.get('state',{}).get('terminated',{}).get('reason') not in (None,'OOMKilled')]
   assert not unexpected,[(c['name'],c['state']) for c in unexpected]
   terminated=[c for c in statuses if c.get('state',{}).get('terminated',{}).get('reason')=='OOMKilled']
   if mode=='borrow' and len(statuses)==9 and all(c.get('ready') for c in statuses):
    logs=k('logs',name,'-c','slot-0')
    if '"allocated_mb": 96' in logs and all('"allocated_mb": 32' in k('logs',name,'-c','slot-'+str(i)) for i in range(1,8)):break
   elif mode=='individual' and any(c['name']=='slot-0' for c in terminated):
    if len(statuses)==9 and all(c.get('ready') for c in statuses if c['name']!='slot-0') and all('"allocated_mb": 32' in k('logs',name,'-c','slot-'+str(i)) for i in range(1,8)):break
   elif mode=='aggregate' and terminated:break
   time.sleep(1)
  uid=pod['metadata']['uid'];matches=run(docker+['find','/sys/fs/cgroup','-type','d','-name','*pod'+uid.replace('-','_')+'.slice']).splitlines();assert len(matches)==1,matches
  group=read_group(matches[0]);assert int(group['memory.max'])==512*2**20
  result={'mode':mode,'pod':name,'uid':uid,'parent':group,'oom_containers':[c['name'] for c in terminated]}
  if mode=='borrow':
   child=json.loads(k('exec',name,'-c','slot-0','--','python3','-c',"import pathlib,json;p=pathlib.Path('/sys/fs/cgroup');print(json.dumps({k:(p/k).read_text().strip() for k in ['memory.max','memory.min','memory.current']}))"))
   assert int(child['memory.max'])==128*2**20 and int(child['memory.min'])==32*2**20 and int(child['memory.current'])>64*2**20
   assert int(group['memory.current'])<512*2**20 and group['events'].get('oom',0)==0
   result['borrower']=child
  elif mode=='individual':
   assert len(terminated)==1 and all(c.get('ready') for c in statuses if c['name']!='slot-0')
   assert group['events'].get('oom',0)==0,'Individual limit test unexpectedly hit parent limit'
  else:
   assert group['events'].get('oom',0)>0,'Aggregate OOM did not occur at parent cgroup'
  report['cases'].append(result)
  k('delete','pod',name,'--wait=true','--timeout=40s');created.remove(name)
 report['passed']=True
finally:
 for name in created:
  try:k('delete','pod',name,'--wait=true','--timeout=40s')
  except Exception:pass
 (a.out/'memory-acceptance.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report),flush=True)
