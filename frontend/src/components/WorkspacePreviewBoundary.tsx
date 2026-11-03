import { Component, type ReactNode } from 'react';

// A failed renderer must not unmount the Team, task composer or file editor.
export default class WorkspacePreviewBoundary extends Component<{ content: string; children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  render() {
    if (!this.state.failed) return this.props.children;
    return <div><p role="status" className="mb-3 text-sm text-amber-700">格式化预览暂不可用，以下为文件原文。可以复制或下载，团队页面仍可继续使用。</p><pre className="whitespace-pre-wrap break-words rounded-xl bg-white p-4 text-sm">{this.props.content}</pre></div>;
  }
}
