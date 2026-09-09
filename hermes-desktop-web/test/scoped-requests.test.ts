import assert from 'node:assert/strict'
import test from 'node:test'
import { scopedClarifyParams } from '../src/scoped-requests.ts'

test('clarify answers and skips carry the explicit session without mutating caller data', () => {
  const params = { request_id: 'request-a', question_id: 'q1', answer: 'yes' }
  assert.deepEqual(scopedClarifyParams('session-a', params), { ...params, session_id: 'session-a' })
  assert.equal('session_id' in params, false)
  assert.deepEqual(scopedClarifyParams('session-a', { request_id: 'request-a', answer: '' }), {
    request_id: 'request-a', answer: '', session_id: 'session-a'
  })
})

test('clarify does not guess a missing session or replace a conflicting owner', () => {
  for (const id of [undefined, null, '', ' ', 1, ' session-a']) assert.throws(() => scopedClarifyParams(id, {}))
  assert.throws(() => scopedClarifyParams('session-a', { session_id: 'session-b' }), /does not match/)
})
