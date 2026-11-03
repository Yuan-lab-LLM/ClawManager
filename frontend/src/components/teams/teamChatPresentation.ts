export type ChatAuthorType = 'member'|'user'|'system'|'unknown';
export function chatAuthorType(senderKey: string, sourceKey: string, knownMember: boolean, systemProcess: boolean): ChatAuthorType {
  if (systemProcess) return 'system';
  const platform = ['clawmanager', 'ClawManager', 'clawmanager-monitor', 'system'].includes(sourceKey || senderKey);
  // A platform-forwarded actor may share a string with a member ID. The actual
  // source remains distinct; root human messages already carry explicit 'user'.
  if (platform && sourceKey !== senderKey) return 'system';
  if (knownMember) return 'member';
  if (platform) return 'system';
  return /^user(?:-\d+)?$/.test(senderKey) ? 'user' : 'unknown';
}
export type ChatPresentationContext = {
  assignmentId?: string;
  phaseId?: string;
  revision?: number;
  dispatchTarget?: string;
  dispatchAssignmentId?: string;
  dispatchPhaseId?: string;
};
type SegmentMessage = { id: string; kind: string; senderKey: string; authorType?: ChatAuthorType; authorKey?: string; threadKey?: string; segmentKey?: string; presentationContext?: ChatPresentationContext; tone?: string; time: number };
type ContextBoundary = 'dispatch'|'work'|'phase'|'revision';
type SegmentDecision = { startsSegment: boolean; contextBoundary?: ContextBoundary };

// Presentation only. A dispatch envelope describes the RECIPIENT's work, not
// a change in the author's own assignment. This applies to any member/peer.
export function chatPresentationContext(payload: Record<string, unknown>, step: Record<string, unknown> | undefined, senderKey: string, targetKey: string, assignmentEvent: boolean, feedbackEvent: boolean): ChatPresentationContext {
  const text = (record: Record<string, unknown> | undefined, keys: string[]) => {
    for (const key of keys) {
      const value = record?.[key];
      if ((typeof value === 'string' && value.trim()) || typeof value === 'number') return String(value).trim();
    }
    return undefined;
  };
  const read = (keys: string[]) => text(payload, keys) || text(step, keys);
  const assignmentId = read(['assignmentId', 'assignment_id']);
  const phaseId = read(['phaseId', 'phase_id']);
  const rawRevision = read(['revision']);
  const revision = rawRevision !== undefined && Number.isFinite(Number(rawRevision)) ? Number(rawRevision) : undefined;
  const dispatch = !feedbackEvent && !!targetKey && senderKey !== targetKey
    && (assignmentEvent || text(step, ['type'])?.toLowerCase() === 'assignment');
  return dispatch ? { dispatchTarget: targetKey, dispatchAssignmentId: assignmentId, dispatchPhaseId: phaseId }
    : { assignmentId, phaseId, revision };
}

function segmentDecision(previous: SegmentMessage | undefined, current: SegmentMessage, ownContext: ChatPresentationContext): SegmentDecision {
  const start: SegmentDecision = { startsSegment: true };
  if (!previous || current.kind === 'system' || previous.kind === 'system') return start;
  // Human submissions are deliberately always independent, even if adjacent.
  if (current.authorType !== 'member' || previous.authorType !== 'member') return start;
  if ((current.authorKey || current.senderKey) !== (previous.authorKey || previous.senderKey)) return start;
  if (!current.threadKey || current.threadKey !== previous.threadKey) return start;
  const hardTone = (message: SegmentMessage) => ['feedback','warning','error'].includes(message.tone || '')
    || (message.tone === 'assignment' && !message.presentationContext?.dispatchTarget);
  if (hardTone(current) || hardTone(previous)) return start;
  const gap = current.time - previous.time;
  if (!Number.isFinite(gap) || gap < 0 || gap > 5 * 60 * 1000 || new Date(current.time).toDateString() !== new Date(previous.time).toDateString()) return start;
  const context = current.presentationContext;
  if (!context) return current.segmentKey !== previous.segmentKey ? start : { startsSegment: false };
  if (context.dispatchTarget) return { startsSegment: false, contextBoundary: 'dispatch' };
  if (context.assignmentId && ownContext.assignmentId && context.assignmentId !== ownContext.assignmentId) return { startsSegment: true, contextBoundary: 'work' };
  if (context.revision !== undefined && ownContext.revision !== undefined && context.revision !== ownContext.revision) return { startsSegment: true, contextBoundary: 'revision' };
  if (context.phaseId && ownContext.phaseId && context.phaseId !== ownContext.phaseId) return { startsSegment: false, contextBoundary: 'phase' };
  return { startsSegment: false };
}

// Flat message nodes and IDs stay intact. Only the repeated header is elided;
// this is not another dedupe/filter/sort or a business-state projection.
export function startsCollaborationSegment(previous: SegmentMessage | undefined, current: SegmentMessage) {
  return segmentDecision(previous, current, previous?.presentationContext || {}).startsSegment;
}

// Remember only explicit own-work fields within an adjacent author segment.
// An intervening dispatch/missing field cannot erase an actual retry boundary.
// Messages, IDs, content, sorting/dedupe and protocol fields are never mutated.
export function buildCollaborationPresentation<T extends SegmentMessage>(messages: T[]) {
  let previous: T | undefined;
  let ownContext: ChatPresentationContext = {};
  return messages.map(message => {
    const decision = segmentDecision(previous, message, ownContext);
    if (decision.startsSegment) ownContext = {};
    if (message.authorType === 'member' && !message.presentationContext?.dispatchTarget) {
      const context = message.presentationContext;
      if (context?.assignmentId) ownContext.assignmentId = context.assignmentId;
      if (context?.phaseId) ownContext.phaseId = context.phaseId;
      if (context?.revision !== undefined) ownContext.revision = context.revision;
    }
    previous = message;
    return { message, ...decision };
  });
}
