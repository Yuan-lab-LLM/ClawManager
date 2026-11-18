import type { GitChange } from '../../types/workbench';

export function changeStats(change: GitChange, staged: boolean) {
  if (change.binary || change.stats_known === false) return null;
  const additions = staged ? change.staged_additions : change.worktree_additions;
  const deletions = staged ? change.staged_deletions : change.worktree_deletions;
  if (additions !== undefined && deletions !== undefined) return { additions, deletions };
  if (change.staged && change.worktree_status.trim() && !change.untracked) return null;
  if (change.untracked && change.stats_known !== true && !change.additions && !change.deletions) return null;
  return { additions: change.additions, deletions: change.deletions };
}
export function changePaths(change: GitChange, staged: boolean) {
  const renamed = (staged ? change.index_status : change.worktree_status) === 'R';
  return renamed && change.previous_path ? [change.previous_path, change.path] : [change.path];
}
export function bulkPaths(changes: GitChange[], staged: boolean) {
  const paths = [...new Set(changes.flatMap(change => changePaths(change, staged)))];
  return paths.length > 1000 ? null : paths;
}
export interface DiffLine {
  text: string;
  kind: 'meta' | 'hunk' | 'context' | 'addition' | 'deletion';
  oldLine?: number;
  newLine?: number;
}
export function parseUnifiedDiff(diff: string): DiffLine[] {
  let oldLine = 0, newLine = 0, oldRemaining = 0, newRemaining = 0;
  return diff.replace(/\n$/, '').split('\n').filter((_, i) => diff.length > 0 || i > 0).map(text => {
    const hunk = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@/.exec(text);
    if (hunk) {
      oldLine = Number(hunk[1]); newLine = Number(hunk[3]);
      oldRemaining = Number(hunk[2] ?? 1); newRemaining = Number(hunk[4] ?? 1);
      return { text, kind: 'hunk' };
    }
    if (oldRemaining || newRemaining) {
      if (text.startsWith('+') && newRemaining) { newRemaining--; return { text, kind: 'addition', newLine: newLine++ }; }
      if (text.startsWith('-') && oldRemaining) { oldRemaining--; return { text, kind: 'deletion', oldLine: oldLine++ }; }
      if (text.startsWith(' ') && oldRemaining && newRemaining) {
        oldRemaining--; newRemaining--; return { text, kind: 'context', oldLine: oldLine++, newLine: newLine++ };
      }
    }
    return { text, kind: 'meta' };
  });
}
export function imagePreviewPath(repo: string, file: string, previewPath?: string) {
  const path = previewPath ?? [repo, file].filter(Boolean).join('/');
  if (path.startsWith('/') || path.split('/').includes('..') || !/\.(png|jpe?g|gif|webp)$/i.test(path)) return null;
  return path;
}
export const safeImageMime = (mime: string) => /^image\/(png|jpeg|gif|webp)$/i.test(mime);
