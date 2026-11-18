import type {BackupConfig, SecretReference} from '../../../services/systemBackupService';
import type {backupMessages} from './messages';

type Props = {
  config: BackupConfig; onChange: (config: BackupConfig) => void;
  disabled: boolean; securityAdmin: boolean; text: ReturnType<typeof backupMessages>;
};

export default function BackupConfigFields({config,onChange,disabled,securityAdmin,text}: Props) {
 const inputClass='mt-2 w-full rounded-lg border border-slate-200 px-3 py-2';
 const numberFields: Array<[keyof BackupConfig,string,number,number]> = [
  ['gate_acquisition_timeout_seconds',text.gateTimeout,10,600],['quiesce_max_hold_seconds',text.holdTimeout,30,3600],
  ['task_deadline_seconds',text.taskTimeout,600,43200],['max_artifact_bytes',text.artifactBytes,1048576,900*2**30],
  ['staging_capacity_bytes',text.stagingBytes,2**30,2**40],['max_index_bytes',text.indexBytes,1048576,8*2**30],['max_source_items',text.sourceItems,1,100000000],
 ];
 const purposes = {artifact_target:'artifact',encryption_key:'encryption',signing_key:'signing',catalog:'catalog'};
 const labels = {artifact_target:text.artifactTarget,encryption_key:text.encryptionKey,signing_key:text.signingKey,catalog:text.catalog};
 const changeRef=(name:keyof typeof purposes,field:keyof SecretReference,value:string)=>{
  const old=config[name]??{provider:'kubernetes',name:'',version:'',purpose:purposes[name]};
  onChange({...config,[name]:{...old,[field]:value}});
 };
 return <>
  <label className="block text-sm">{text.profile}<select className={inputClass} disabled={disabled||!securityAdmin} value={config.profile} onChange={e=>onChange({...config,profile:e.target.value})}><option value="single-node">single-node</option><option value="cluster">cluster</option></select></label>
  <fieldset className="space-y-4 rounded-lg border border-slate-200 p-4" disabled={disabled||!securityAdmin}><legend className="px-2 text-sm font-medium">{text.storage}</legend>
   {(Object.keys(purposes) as Array<keyof typeof purposes>).map(name=><div key={name} className="space-y-2"><p className="text-sm font-medium">{labels[name]}</p><label className="block text-xs">{text.provider}<select className={inputClass} value={config[name]?.provider??'kubernetes'} onChange={e=>changeRef(name,'provider',e.target.value)}><option value="kubernetes">Kubernetes</option><option value="vault">Vault</option></select></label><label className="block text-xs">{text.referenceName}<input className={inputClass} maxLength={128} value={config[name]?.name??''} onChange={e=>changeRef(name,'name',e.target.value)}/></label><label className="block text-xs">{text.referenceVersion}<input className={inputClass} maxLength={128} value={config[name]?.version??''} onChange={e=>changeRef(name,'version',e.target.value)}/></label></div>)}
  </fieldset>
  <details><summary className="cursor-pointer text-sm font-medium">{text.limits}</summary><div className="mt-4 space-y-4">{numberFields.map(([key,label,min,max])=><label key={key} className="block text-sm">{label}<input className={inputClass} type="number" min={min} max={max} disabled={disabled} value={Number(config[key])} onChange={e=>onChange({...config,[key]:Number(e.target.value)})}/></label>)}</div></details>
 </>;
}
