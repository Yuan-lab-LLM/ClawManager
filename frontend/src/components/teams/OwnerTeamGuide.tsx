import {useId, useRef, useState} from 'react';
import {BookOpen} from 'lucide-react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import {OwnerTeamDialog} from './OwnerTeamDialog';
import {ownerTeamGuideChapters as chapters} from './ownerTeamGuideContent';

function GuideDialog({onClose}: {onClose: () => void}) {
 const [selected,setSelected] = useState(0);
 const article = useRef<HTMLElement>(null);
 const id = useId();
 const chapter = chapters[selected];
 const select = (index: number, focus=false) => {
  setSelected(index);
  article.current?.scrollTo({top:0});
  if (focus) document.getElementById(`${id}-tab-${index}`)?.focus();
 };
 return <OwnerTeamDialog title="Team 使用指南" onClose={onClose}>
  <div className="ot-guide-layout">
   <nav role="tablist" aria-orientation="vertical" aria-label="使用指南目录">
    {chapters.map(({title},index)=><button type="button" role="tab" id={`${id}-tab-${index}`} key={title} aria-selected={selected===index} tabIndex={selected===index?0:-1} aria-controls={`${id}-chapter`} onClick={()=>select(index)} onKeyDown={event=>{
     const next = event.key==='ArrowDown'?(index+1)%chapters.length:event.key==='ArrowUp'?(index+chapters.length-1)%chapters.length:event.key==='Home'?0:event.key==='End'?chapters.length-1:null;
     if(next!==null){event.preventDefault();select(next,true)}
    }}>{title}</button>)}
   </nav>
   <article ref={article} role="tabpanel" tabIndex={0} id={`${id}-chapter`} aria-labelledby={`${id}-tab-${selected}`}>
    <h4>{chapter.title}</h4>
    <ReactMarkdown remarkPlugins={[remarkGfm]} components={{table:({children})=><div className="ot-guide-table"><table>{children}</table></div>}}>{chapter.content}</ReactMarkdown>
   </article>
  </div>
 </OwnerTeamDialog>;
}

export function OwnerTeamGuide() {
 const [open,setOpen] = useState(false);
 return <>
  <button type="button" className="ot-secondary" aria-haspopup="dialog" onClick={()=>setOpen(true)}><BookOpen size={15} aria-hidden="true"/>使用指南</button>
  {open && <GuideDialog onClose={()=>setOpen(false)}/>}
 </>;
}
