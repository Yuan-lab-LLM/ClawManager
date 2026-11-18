import { useEffect, useState } from 'react';

export const beforePageRefresh = 'clawmanager:before-page-refresh';

// Checking the actual module entry detects UI updates even when the API version
// is unchanged. Never automatically reload or replay an operation.
export default function PageVersionNotice() {
  const [changed, setChanged] = useState(false);
  const [blocked, setBlocked] = useState(false);
  useEffect(() => {
    const entry = document.querySelector<HTMLScriptElement>('script[type="module"][src]')?.getAttribute('src');
    if (!entry) return;
    let busy = false;
    const controller = new AbortController();
    const check = async () => {
      if (busy || document.hidden) return;
      busy = true;
      try {
        const response = await fetch('/index.html', { cache: 'no-store', signal: controller.signal, credentials: 'same-origin' });
        if (!response.ok || !response.headers.get('Content-Type')?.includes('text/html')) return;
        const doc = new DOMParser().parseFromString(await response.text(), 'text/html');
        const next = doc.querySelector('script[type="module"][src]')?.getAttribute('src');
        if (next && next !== entry && !controller.signal.aborted) setChanged(true);
      } catch { /* Offline checks do not interrupt the current workspace. */ }
      finally { busy = false; }
    };
    void check();
    const focus = () => void check();
    window.addEventListener('focus', focus);
    const timer = window.setInterval(focus, 60_000);
    return () => { controller.abort(); window.removeEventListener('focus', focus); window.clearInterval(timer); };
  }, []);
  if (!changed) return null;
  return <div role="status" data-page-version-notice className="fixed bottom-4 left-4 right-4 z-[200] flex flex-wrap items-center justify-between gap-3 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-950 shadow-lg">
    <span>{blocked ? '请先保存文件、复制或处理未发送内容，再刷新。当前内容已保留。' : '页面已有更新，当前内容已保留。可在处理完手头内容后刷新。'}</span>
    <button className="app-button-secondary" onClick={() => {
      if (!window.dispatchEvent(new Event(beforePageRefresh, { cancelable: true }))) { setBlocked(true); return; }
      window.location.reload();
    }}>刷新页面</button>
  </div>;
}
