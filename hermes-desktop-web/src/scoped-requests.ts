/** Add an explicit owner to the upstream clarify wire request; never infer one. */
export function scopedClarifyParams(sessionId: unknown, params: Record<string, unknown>): Record<string, unknown> {
  if (typeof sessionId !== 'string' || !sessionId.trim() || sessionId !== sessionId.trim()) {
    throw new Error('Clarification requires an active session')
  }
  if (params.session_id !== undefined && params.session_id !== sessionId) {
    throw new Error('Clarification session does not match its owner')
  }
  return { ...params, session_id: sessionId }
}
