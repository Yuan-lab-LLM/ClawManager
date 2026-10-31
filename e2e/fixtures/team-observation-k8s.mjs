// Disposable Kubernetes observation boundary for exact-App-image HTTP tests.
import http from 'node:http';
const pools=['openclaw','hermes'];
const deployments=pools.map(type=>({apiVersion:'apps/v1',kind:'Deployment',metadata:{name:type,namespace:'owner-team-isolated',uid:`dep-${type}`,labels:{'clawmanager.io/runtime-type':type}},spec:{replicas:1,selector:{matchLabels:{app:type}},template:{metadata:{labels:{app:type}},spec:{containers:[{name:'runtime',image:`registry/${type}:fixture`}]}}},status:{readyReplicas:1}}));
const replicas=pools.map(type=>({apiVersion:'apps/v1',kind:'ReplicaSet',metadata:{name:`rs-${type}`,namespace:'owner-team-isolated',uid:`rs-${type}`,ownerReferences:[{kind:'Deployment',name:type,uid:`dep-${type}`,controller:true}]}}));
const pods=pools.map((type,index)=>({apiVersion:'v1',kind:'Pod',metadata:{name:`pod-${type}`,namespace:'owner-team-isolated',uid:`pod-${type}`,labels:{app:type},ownerReferences:[{kind:'ReplicaSet',name:`rs-${type}`,uid:`rs-${type}`,controller:true}]},spec:{containers:[{name:'runtime',image:`registry/${type}:fixture`}]},status:{phase:'Running',podIP:`10.1.2.${index+3}`,conditions:[{type:'Ready',status:'True'}],containerStatuses:[{name:'runtime',ready:true,restartCount:0,image:`registry/${type}:fixture`,imageID:`registry/${type}:fixture`,state:{running:{startedAt:'2026-01-01T00:00:00Z'}}}]}}));
const leases=new Map();
http.createServer((req,res)=>{
 const url=new URL(req.url,'http://fixture');let body;
 if(url.pathname.includes('/leases')){
  if(req.method==='POST'||req.method==='PUT'){
   let data='';req.on('data',chunk=>data+=chunk);req.on('end',()=>{try{const lease=JSON.parse(data);lease.metadata.resourceVersion=String(Number(lease.metadata.resourceVersion||0)+1);const key=url.pathname.endsWith('/leases')?url.pathname+'/'+lease.metadata.name:url.pathname;leases.set(key,lease);res.writeHead(200,{'Content-Type':'application/json'});res.end(JSON.stringify(lease))}catch{res.writeHead(415);res.end()}});return;
  }
  body=leases.get(url.pathname);
  if(body){res.writeHead(200,{'Content-Type':'application/json'});res.end(JSON.stringify(body));return}
 }
 if(req.method==='GET'){
  if(url.pathname.endsWith('/deployments'))body={apiVersion:'apps/v1',kind:'DeploymentList',items:deployments};
  else if(url.pathname.endsWith('/replicasets'))body={apiVersion:'apps/v1',kind:'ReplicaSetList',items:replicas};
  else if(url.pathname.endsWith('/pods'))body={apiVersion:'v1',kind:'PodList',items:pods.filter(p=>!url.searchParams.get('labelSelector')||url.searchParams.get('labelSelector')===`app=${p.metadata.labels.app}`)};
  else body=deployments.find(d=>url.pathname.endsWith(`/deployments/${d.metadata.name}`))||replicas.find(r=>url.pathname.endsWith(`/replicasets/${r.metadata.name}`));
 }
 if(body){res.writeHead(200,{'Content-Type':'application/json'});res.end(JSON.stringify(body));return}
 res.writeHead(404,{'Content-Type':'application/json'});res.end(JSON.stringify({apiVersion:'v1',kind:'Status',status:'Failure',reason:'NotFound',code:404}));
}).listen(8080);
