import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { basicSetup, EditorView } from "codemirror";
import { Compartment, EditorState } from "@codemirror/state";
import { languages } from "@codemirror/language-data";
import { LanguageDescription } from "@codemirror/language";
import { keymap } from "@codemirror/view";
import { load as loadYAML } from "js-yaml";
import { Copy, Download, FilePenLine, Save, TextCursorInput, X } from "lucide-react";
import type { WorkspaceFileOperations } from "./WorkspaceFileManager";

export interface WorkspaceText { text: string; version: string }
export type WorkspaceTextOperations = Pick<WorkspaceFileOperations, "readText" | "saveText" | "rename">;
export function editableWorkspaceName(name: string) {
  return /\.(txt|md|json|ya?ml|log|py|js|ts|go|sh|toml|ini|conf|cfg)$/i.test(name) || /^\.env(?:\..+)?$/i.test(name) || /^(Dockerfile|Makefile)$/i.test(name);
}
function message(error: unknown) {
  return (error as {response?:{data?:{error?:string}}})?.response?.data?.error || (error instanceof Error ? error.message : "操作失败，请重试。当前编辑内容已保留。");
}

export function WorkspaceTextEditor({ instanceId, initialPath, service, onClose, onSaved, canWrite, shared = false }: {
  instanceId:number; initialPath:string; service:WorkspaceTextOperations; onClose:()=>void; onSaved:()=>Promise<void>; canWrite:boolean; shared?:boolean;
}) {
  const [path,setPath]=useState(initialPath);
  const [doc,setDoc]=useState<WorkspaceText|null>(null);
  const [text,setText]=useState("");
  const [error,setError]=useState<string|null>(null);
  const [notice,setNotice]=useState("");
  const [busy,setBusy]=useState(false);
  const [wrap,setWrap]=useState(true);
  const [generation,setGeneration]=useState(0);
  const [remoteChanged,setRemoteChanged]=useState(false);
  const [refreshError,setRefreshError]=useState("");
  const operating=useRef(false);
  const mounted=useRef(true);
  const documentRef=useRef(doc);useLayoutEffect(()=>{documentRef.current=doc},[doc]);
  const host=useRef<HTMLDivElement>(null);
  const dialog=useRef<HTMLDialogElement>(null);
  const view=useRef<EditorView|null>(null);
  const wrapping=useRef(new Compartment());
  const readonly=useRef(new Compartment());
  const dirty=doc!==null&&text!==doc.text;
  const close=()=>{if(!operating.current&&(!dirty||window.confirm("文件尚未保存，确定放弃修改并关闭？")))onClose()};
  const closeRef=useRef(close);useLayoutEffect(()=>{closeRef.current=close});

  useEffect(()=>{mounted.current=true;const previous=document.activeElement as HTMLElement|null;const modal=dialog.current;modal?.showModal();return()=>{mounted.current=false;modal?.close();previous?.focus()}},[]);
  useEffect(()=>{
    const handler=(event:BeforeUnloadEvent)=>{if(dirty||busy){event.preventDefault();event.returnValue=""}};
    window.addEventListener("beforeunload",handler);return()=>window.removeEventListener("beforeunload",handler);
  },[dirty,busy]);
  useEffect(()=>{
    // Chrome's same-document navigation does not fire beforeunload.
    const navigation=(window as unknown as {navigation?:EventTarget}).navigation;
    const guard=(event:Event)=>{
      const destination=(event as Event&{destination?:{sameDocument?:boolean}}).destination;
      if(event.cancelable&&destination?.sameDocument&&(dirty||busy)&&!window.confirm("文件尚未保存或正在处理，确定离开并放弃当前编辑？"))event.preventDefault();
    };
    navigation?.addEventListener("navigate",guard);return()=>navigation?.removeEventListener("navigate",guard);
  },[dirty,busy]);

  const reload=useCallback(async()=>{
    if(operating.current)return;
    operating.current=true;
    setBusy(true);setError(null);
    try {const next=await service.readText!(instanceId,path);if(!mounted.current)return;if(/\r(?!\n)/.test(next.text)||(/\r\n/.test(next.text)&&/(?<!\r)\n/.test(next.text)))throw new Error("文件包含混合或特殊换行符，请下载后编辑，避免自动转换原文件格式。");setDoc(next);setText(next.text);setRemoteChanged(false);setRefreshError("");setGeneration(g=>g+1)}
    catch(e){if(mounted.current)setError(message(e))}finally{operating.current=false;if(mounted.current)setBusy(false)}
  },[instanceId,path,service]);
  useEffect(()=>{let active=true;queueMicrotask(()=>{if(active)void reload()});return()=>{active=false}},[reload]);
  useEffect(()=>{
    let active=true,inFlight=false;const abort=new AbortController();
    const check=async()=>{
      if(!active||inFlight||operating.current||document.hidden||!documentRef.current)return;
      inFlight=true;
      const baseVersion=documentRef.current.version;
      try{const next=await service.readText!(instanceId,path,abort.signal);if(active&&!operating.current&&baseVersion===documentRef.current?.version){setRemoteChanged(next.version!==documentRef.current?.version);setRefreshError("")}}
      catch(e){if(active)setRefreshError(`检查远端文件失败：${message(e)}；本地草稿保留。`)}finally{inFlight=false}
    };
    const timer=window.setInterval(()=>void check(),5000);
    const focus=()=>void check();window.addEventListener("focus",focus);
    return()=>{active=false;abort.abort();window.clearInterval(timer);window.removeEventListener("focus",focus)};
  },[instanceId,path,service]);

  const save=async()=>{
    if(!doc||operating.current||!canWrite)return;
    setError(null);setNotice("");
    try {
      const clean=text.replace(/^\uFEFF/,"");
      if(/\.json$/i.test(path))JSON.parse(clean);
      if(/\.ya?ml$/i.test(path))loadYAML(clean);
    }catch(e){setError(`语法检查未通过：${message(e)}`);return}
    operating.current=true;setBusy(true);
    try {
      const next=await service.saveText!(instanceId,path,text,doc.version);
      if(!mounted.current)return;
      setDoc(next);setText(next.text);setNotice("已保存。不会自动重启实例或执行脚本。");
      setRemoteChanged(false);
      try{await onSaved()}catch(e){setRefreshError(`文件已保存，但列表刷新失败：${message(e)}。请刷新列表，不必重复保存。`)}
    }catch(e){if(mounted.current)setError(message(e))}finally{operating.current=false;if(mounted.current)setBusy(false)}
  };
  const saveRef=useRef(save);useLayoutEffect(()=>{saveRef.current=save});

  useEffect(()=>{
    if(!doc||!host.current)return;
    const original=doc.text;
    const bom=original.startsWith("\uFEFF")?"\uFEFF":"";
    const eol=original.includes("\r\n")?"\r\n":"\n";
    const language=new Compartment();
    const editor=new EditorView({parent:host.current,state:EditorState.create({doc:original.replace(/^\uFEFF/,""),extensions:[
      basicSetup, language.of([]), wrapping.current.of(EditorView.lineWrapping), readonly.current.of(EditorState.readOnly.of(false)),
      EditorView.contentAttributes.of({"aria-label":"文件内容编辑器",spellcheck:"false"}),
      keymap.of([{key:"Mod-s",run:()=>{void saveRef.current();return true}}]),
      EditorView.updateListener.of(update=>{if(update.docChanged)setText(bom+update.state.doc.toString().replace(/\n/g,eol))}),
      EditorView.theme({"&":{height:"100%",fontSize:"14px"},".cm-scroller":{overflow:"auto",fontFamily:"Consolas, monospace"}}),
    ]})});
    view.current=editor;
    let alive=true;
    const desc=LanguageDescription.matchFilename(languages,path);
    if(desc)void desc.load().then(support=>{if(alive)editor.dispatch({effects:language.reconfigure(support)})}).catch(()=>undefined);
    editor.focus();
    return()=>{alive=false;editor.destroy();view.current=null};
    // Recreate only on explicit load/rename, never after typing or a failed save.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  },[generation]);
  useEffect(()=>{view.current?.dispatch({effects:wrapping.current.reconfigure(wrap?EditorView.lineWrapping:[])})},[wrap,generation]);
  useEffect(()=>{view.current?.dispatch({effects:readonly.current.reconfigure(EditorState.readOnly.of(busy||!canWrite))})},[busy,canWrite,generation]);

  const rename=async()=>{
    if(operating.current||!canWrite)return;
    if(dirty){setError("请先保存或重新载入放弃修改，再重命名。");return}
    const old=path.split("/").pop()!;
    const name=window.prompt("新文件名（不会覆盖同名文件）",old)?.trim();
    if(!name||name===old)return;
    if(/[\\/]/.test(name)||name.includes(String.fromCharCode(0))||name==="."||name===".."){setError("文件名无效。");return}
    if(!editableWorkspaceName(name)){setError("请保留支持编辑的文本文件扩展名；其他名称可关闭编辑器后重命名。");return}
    operating.current=true;setBusy(true);setError(null);
    try{const next=path.slice(0,path.length-old.length)+name;await service.rename(instanceId,path,next);setPath(next);setNotice("已重命名。");try{await onSaved()}catch(e){setRefreshError(`已重命名，但列表刷新失败：${message(e)}`)}}catch(e){setError(message(e))}finally{operating.current=false;setBusy(false)}
  };

  const downloadDraft=()=>{const url=URL.createObjectURL(new Blob([text],{type:"text/plain;charset=utf-8"}));const link=document.createElement("a");link.href=url;link.download=path.split("/").pop()||"draft.txt";link.click();setTimeout(()=>URL.revokeObjectURL(url),1000)};

  return <dialog ref={dialog} data-workspace-editor onCancel={e=>{e.preventDefault();closeRef.current()}} className="m-auto h-[min(85dvh,850px)] w-[min(94vw,1100px)] max-w-none rounded-2xl border border-slate-200 p-0 shadow-2xl backdrop:bg-slate-950/40" aria-labelledby="workspace-editor-title">
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex items-center justify-between gap-4 border-b px-5 py-4">
        <div className="min-w-0"><h2 id="workspace-editor-title" className="flex items-center gap-2 text-lg font-semibold"><FilePenLine size={20}/>编辑文件 {dirty&&<span className="text-xs text-amber-700">未保存</span>}</h2><p className="mt-1 truncate text-sm text-slate-500" title={path}>{path}</p></div>
        <button className="cm-icon-button" aria-label="关闭编辑器" disabled={busy} onClick={close}><X size={18}/></button>
      </header>
      <div className="flex flex-wrap items-center gap-4 px-5 py-2 text-sm">
        <button className="app-button-secondary" disabled={!doc||busy||!canWrite} onClick={()=>void rename()}><TextCursorInput size={16}/>重命名</button>
        <label className="flex items-center gap-2"><input type="checkbox" checked={wrap} onChange={e=>setWrap(e.target.checked)}/>自动换行</label>
        <button className="app-button-secondary" disabled={!doc} onClick={()=>void navigator.clipboard.writeText(text).then(()=>setNotice("已复制当前草稿。"),()=>setError("复制失败，请下载草稿。"))}><Copy size={16}/>复制草稿</button>
        <button className="app-button-secondary" disabled={!doc} onClick={downloadDraft}><Download size={16}/>下载草稿</button>
        <span className="text-xs text-slate-500">Ctrl+F 查找 · Ctrl+S 保存 · UTF-8 · 最大 1 MiB</span>
        <button className="ml-auto text-blue-600" disabled={busy} onClick={()=>{if(!dirty||confirm("重新载入会放弃当前修改，是否继续？"))void reload()}}>重新载入</button>
      </div>
      {error&&<p role="alert" className="mx-5 mb-2 rounded-lg bg-red-50 p-3 text-sm text-red-700">{error}</p>}
      {notice&&<p role="status" className="mx-5 mb-2 text-sm text-emerald-700">{notice}</p>}
      {remoteChanged&&<p role="status" className="mx-5 mb-2 rounded-lg bg-amber-50 p-3 text-sm text-amber-800">远端文件已更新，当前草稿不会被覆盖。请复制或下载草稿，再重新载入后合并；保存时会检查版本冲突。</p>}
      {refreshError&&<p role="status" className="mx-5 mb-2 text-sm text-amber-700">{refreshError}</p>}
      {!canWrite&&<p className="mx-5 mb-2 text-sm text-amber-700">维护操作期间暂不可保存，当前编辑内容保留。</p>}
      <div ref={host} className="min-h-0 flex-1 border-y bg-white"/>
      <footer className="flex items-center justify-between gap-3 px-5 py-3"><span className="min-w-0 text-xs text-slate-500">{shared?"共享产物使用显式保存；Worker 不参与编辑锁，活跃写入时请先等待交付。":"配置文件保存后可能需要手动重启。"}内容不写入浏览器持久缓存。</span><button className="app-button-primary shrink-0 whitespace-nowrap" disabled={!doc||busy||!canWrite||!dirty} onClick={()=>void save()}><Save size={16}/>{busy?"处理中…":"保存"}</button></footer>
    </div>
  </dialog>;
}
