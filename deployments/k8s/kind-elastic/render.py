#!/usr/bin/env python3
"""Render a fresh single-node development deployment; never read production Secrets."""
import argparse,json,os,pathlib,re,secrets
import bcrypt
import yaml

p=argparse.ArgumentParser();p.add_argument('--images',type=pathlib.Path,required=True);p.add_argument('--backend-image',type=pathlib.Path,required=True);p.add_argument('--out',type=pathlib.Path,required=True);args=p.parse_args()
os.umask(0o077);args.out.mkdir(parents=True,exist_ok=True)
images=json.loads(args.images.read_text());backend=json.loads(args.backend_image.read_text());assert backend['complete']
credentials=args.out/'credentials.json'
if credentials.exists():local=json.loads(credentials.read_text())
else:
 local={'username':'admin','password':secrets.token_urlsafe(18),'url':'https://localhost:18443','secret_values':{}}
 for key in ['mysql-root-password','mysql-password','jwt-secret','auth-config-encryption-key','runtime-agent-control-token','runtime-agent-report-token','openclaw-gateway-token','minio-root-user','minio-root-password']:
  local['secret_values'][key]=secrets.token_hex(16 if key=='auth-config-encryption-key' else 24)
 local['secret_values']['minio-access-key']=local['secret_values']['minio-root-user'];local['secret_values']['minio-secret-key']=local['secret_values']['minio-root-password']
 credentials.write_text(json.dumps(local,indent=2)+'\n');credentials.chmod(0o600)
password_hash=bcrypt.hashpw(local['password'].encode(),bcrypt.gensalt()).decode()
login=args.out/'admin-login.private.txt';login.write_text('URL: '+local['url']+'\nUsername: '+local['username']+'\nPassword: '+local['password']+'\n');login.chmod(0o600)
base=pathlib.Path(__file__).resolve().parent.parent/'single-node/clawmanager.yaml'
objects=[]
for obj in yaml.safe_load_all(base.read_text()):
 if not obj:continue
 kind=obj['kind'];name=obj.get('metadata',{}).get('name','')
 if kind=='Deployment' and name in [k+'-runtime' for k in ['openclaw','hermes','opencode','deepseek-harness']]:continue
 if kind=='Secret' and name=='clawmanager-secrets':obj['stringData'].update(local['secret_values'])
 if kind=='ConfigMap' and name=='clawmanager-mysql-init':
  for key,value in obj['data'].items():obj['data'][key]=value.replace('$2a$10$pbenze514mwv3pvQySQBVOsF5J4DBXL2kVo1hLa8JFhQu5x3AKvBi',password_hash)
 if kind=='Deployment':
  for c in obj['spec']['template']['spec']['containers']:
   env={e['name']:e for e in c.get('env',[])}
   def setenv(k,v):env[k]={'name':k,'value':v}
   if name=='clawmanager-app':
    c['image']=backend['image'];c['resources']={'requests':{'cpu':'200m','memory':'256Mi'},'limits':{'cpu':'1','memory':'768Mi'}}
    values={
     'K8S_NAMESPACE':'clawmanager-system','RUNTIME_NAMESPACE':'clawmanager-system','RUNTIME_CONTAINER_SLOTS':'8','RUNTIME_SCHEDULER_ENABLED':'true',
     'RUNTIME_POD_CPU_REQUEST':'1','RUNTIME_POD_CPU_LIMIT':'1','RUNTIME_POD_MEMORY_REQUEST':'1536Mi','RUNTIME_POD_MEMORY_LIMIT':'2Gi',
     'RUNTIME_SLOT_CPU_REQUEST':'25m','RUNTIME_SLOT_CPU_LIMIT':'1','RUNTIME_SLOT_MEMORY_REQUEST':'128Mi','RUNTIME_SLOT_MEMORY_LIMIT':'1536Mi',
     'RUNTIME_MAX_GATEWAYS_PER_POD':'8','RUNTIME_GATEWAY_START_IN_FLIGHT_LIMIT':'8','RUNTIME_POD_USER_NAMESPACES':'true','CLAWMANAGER_OPENCODE_PUBLIC_URL_TEMPLATE':'https://opencode-{instance_id}.127-0-0-1.nip.io:18443',
     'CLAWMANAGER_DEEPSEEK_HARNESS_PUBLIC_URL_TEMPLATE':'https://deepseek-harness-{instance_id}.127-0-0-1.nip.io:18443',
    }
    for k,key in [('openclaw','OPENCLAW_RUNTIME_IMAGE'),('hermes','HERMES_RUNTIME_IMAGE'),('opencode','OPENCODE_RUNTIME_IMAGE'),('deepseek-harness','DEEPSEEK_HARNESS_RUNTIME_IMAGE')]:values[key]=images[k]['image']
    for k,v in values.items():setenv(k,v)
   elif name=='mysql':
    c['image']='172.16.1.12:5010/mysql:8.4.8';c['resources']={'requests':{'cpu':'100m','memory':'256Mi'},'limits':{'cpu':'500m','memory':'768Mi'}}
    c.setdefault('args',[]).extend(['--performance-schema=OFF','--innodb-buffer-pool-size=134217728','--max-connections=60'])
   elif name=='minio':
    c['image']='172.16.1.12:5010/minio:latest';c['resources']={'requests':{'cpu':'50m','memory':'128Mi'},'limits':{'cpu':'500m','memory':'512Mi'}};setenv('GOMEMLIMIT','384MiB')
   elif name=='clawmanager-team-redis':
    c['image']='172.16.1.12:5010/redis:7-alpine';c['resources']={'requests':{'cpu':'25m','memory':'32Mi'},'limits':{'cpu':'250m','memory':'128Mi'}}
   elif name=='skill-scanner':
    c['image']='172.16.1.12:5010/skill-scanner:latest';c['resources']={'requests':{'cpu':'50m','memory':'128Mi'},'limits':{'cpu':'500m','memory':'512Mi'}}
   c['env']=list(env.values())
 objects.append(obj)
objects.append({'apiVersion':'storage.k8s.io/v1','kind':'StorageClass','metadata':{'name':'manual'},'provisioner':'kubernetes.io/no-provisioner','volumeBindingMode':'WaitForFirstConsumer','reclaimPolicy':'Retain'})
manifest=args.out/'deployment.private.json';manifest.write_text(json.dumps({'apiVersion':'v1','kind':'List','items':objects},indent=2)+'\n');manifest.chmod(0o600)
print(json.dumps({'manifest':str(manifest),'url':local['url'],'credentials_file':str(credentials),'local_profile':{'slots':8,'pod_request':'1536Mi','pod_limit':'2Gi','slot_request':'128Mi','slot_limit':'1536Mi'},'formal_profile_changed':False}))
