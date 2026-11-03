import { useId, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { ChevronDown, ChevronUp } from "lucide-react";
import "./collapsible-table.css";

export const TABLE_PREVIEW_HEIGHT = 320;

// Reserve readable metadata columns independently of a verbose description.
// The same rule also applies to arbitrary report tables, not just a roster.
export function markdownColumnWidths(headers: string[], rows: string[][]) {
  const units = (text: string) => [...text].reduce((width, char) => width + (/[^\x00-\x7f]/.test(char) ? 2 : 1), 0);
  return headers.map((header, column) => {
    const longest = rows.reduce((length, row) => Math.max(length, units(row[column] || "")), units(header));
    return Math.max(96, Math.min(longest > 60 ? 400 : 176, longest * 7 + 24));
  });
}

export default function CollapsibleTable({ children, minimumWidth = 0, rowCount }: {
  children: ReactNode;
  minimumWidth?: number;
  rowCount?: number;
}) {
  const id = useId();
  const frame = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const scroll = useRef<HTMLDivElement>(null);
  const [overflowing, setOverflowing] = useState(false);
  const [wide, setWide] = useState(false);
  const [expanded, setExpanded] = useState(false);
  useLayoutEffect(() => {
    const table = content.current?.querySelector("table");
    if (!table) return;
    const measure = () => {
      setOverflowing(table.getBoundingClientRect().height > TABLE_PREVIEW_HEIGHT + 1);
      setWide(!!scroll.current && scroll.current.scrollWidth > scroll.current.clientWidth + 1);
    };
    const observer = new ResizeObserver(measure);
    observer.observe(table);
    if (scroll.current) observer.observe(scroll.current);
    measure();
    return () => observer.disconnect();
  }, []);
  const collapsed = overflowing && !expanded;
  const collapse = () => {
    setExpanded(false);
    // Do not leave the user below the table after removing a tall expansion.
    requestAnimationFrame(() => {
      const host = frame.current;
      const feed = host?.closest<HTMLElement>(".team-chat-feed, .team-preview-body, .cm-tech-workspace");
      if (host && feed) {
        const delta = host.getBoundingClientRect().top - feed.getBoundingClientRect().top;
        if (delta < 0) { feed.scrollTop += delta - 12; feed.dispatchEvent(new Event("scroll")); }
      }
    });
  };
  return <div ref={frame} className="team-collapsible-table" data-collapsed={collapsed}>
    <div ref={scroll} className="team-table-scroll" tabIndex={0} role="region" aria-label="表格，可横向滚动">
      <div ref={content} id={id} className="team-table-clip" style={{ minWidth: minimumWidth || undefined, maxHeight: expanded ? undefined : TABLE_PREVIEW_HEIGHT }}
        onFocusCapture={event => {
          // A keyboard user reaching a clipped artifact link must see it.
          if (collapsed && event.target.getBoundingClientRect().bottom > event.currentTarget.getBoundingClientRect().bottom) setExpanded(true);
        }}>{children}</div>
    </div>
    {(overflowing || wide) && <div className="team-table-footer">
      <span>{rowCount === undefined ? "表格" : `${rowCount} 行`} · {collapsed ? "已折叠部分内容" : "完整内容"}{wide && <small className="block">左右滚动查看其余列</small>}</span>
      {overflowing && <button type="button" aria-expanded={expanded} aria-controls={id} onClick={() => expanded ? collapse() : setExpanded(true)}>
        {expanded ? <ChevronUp size={14} /> : <ChevronDown size={14} />}{expanded ? "收起" : "显示全部"}
      </button>}
    </div>}
  </div>;
}
