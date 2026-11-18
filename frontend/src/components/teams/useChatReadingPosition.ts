import { useCallback, useLayoutEffect, useRef, useState } from "react";

/** Preserve an actual message anchor across polling, prepends and dock reflow. */
export function useChatReadingPosition(lastMessageId?: string, storageKey?: string) {
  const feedRef = useRef<HTMLDivElement>(null);
  const anchor = useRef<{ id?: string; offset: number; bottom: boolean }>({ offset: 0, bottom: true });
  const initialized = useRef(false);
  if (!initialized.current) {
    initialized.current = true;
    try { const saved = storageKey && JSON.parse(sessionStorage.getItem(storageKey) || 'null'); if (saved && typeof saved.bottom === 'boolean' && Number.isFinite(saved.offset)) anchor.current = saved; } catch { /* Presentation only. */ }
  }
  const previousLast = useRef(lastMessageId);
  const [unread, setUnread] = useState(false);
  const restoring = useRef(false);
  const frame = useRef(0);
  const capture = useCallback(() => {
    const feed = feedRef.current;
    if (!feed || restoring.current) return;
    const top = feed.getBoundingClientRect().top;
    const node = [...feed.querySelectorAll<HTMLElement>('[data-chat-message]')]
      .find(item => item.getBoundingClientRect().bottom > top + 1);
    const bottom = feed.scrollHeight - feed.scrollTop - feed.clientHeight < 48;
    anchor.current = { id: node?.dataset.chatMessage, offset: node ? node.getBoundingClientRect().top - top : 0, bottom };
    if (storageKey && node) sessionStorage.setItem(storageKey, JSON.stringify(anchor.current));
    if (bottom) setUnread(false);
  }, [storageKey]);
  const restore = useCallback(() => {
    const feed = feedRef.current;
    if (!feed) return;
    restoring.current = true;
    if (anchor.current.bottom) feed.scrollTop = feed.scrollHeight;
    else {
      const node = [...feed.querySelectorAll<HTMLElement>('[data-chat-message]')]
        .find(item => item.dataset.chatMessage === anchor.current.id);
      if (node) feed.scrollTop += node.getBoundingClientRect().top - feed.getBoundingClientRect().top - anchor.current.offset;
    }
    cancelAnimationFrame(frame.current);
    frame.current = requestAnimationFrame(() => { restoring.current = false; capture(); });
  }, [capture]);
  useLayoutEffect(() => {
    if (previousLast.current && previousLast.current !== lastMessageId && !anchor.current.bottom) setUnread(true);
    previousLast.current = lastMessageId;
    restore();
  });
  useLayoutEffect(() => {
    const feed = feedRef.current;
    if (!feed) return;
    const observer = new ResizeObserver(restore);
    observer.observe(feed);
    if (feed.firstElementChild) observer.observe(feed.firstElementChild);
    return () => { observer.disconnect(); cancelAnimationFrame(frame.current); };
  }, [restore]);
  const jumpToLatest = () => { anchor.current.bottom = true; setUnread(false); restore(); };
  return { feedRef, capture, unread, jumpToLatest };
}
