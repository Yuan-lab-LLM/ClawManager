import assert from 'node:assert/strict'
import test from 'node:test'
import { AssistantRuntimeImpl, fromThreadMessageLike, getAutoStatus } from '@assistant-ui/core/internal'
import type { ExportedMessageRepository, ExternalStoreAdapter, ThreadMessage } from '@assistant-ui/react'
import { IncrementalExternalStoreRuntimeCore } from '../.upstream/source/apps/desktop/src/lib/incremental-external-store-runtime.ts'

const status = getAutoStatus(false, false, false, false, undefined)
const message = (role: 'assistant' | 'user', id: string, text: string): ThreadMessage =>
  fromThreadMessageLike({ role, content: [{ type: 'text', text }] }, id, status)
const repository = (messages: ThreadMessage[]): ExportedMessageRepository => ({
  headId: messages.at(-1)?.id ?? null,
  messages: messages.map((message, index) => ({ message, parentId: index ? messages[index - 1].id : null })),
})
const adapter = (messageRepository: ExportedMessageRepository, isRunning: boolean): ExternalStoreAdapter => ({
  messageRepository, isRunning, setMessages: () => {}, onNew: async () => {}, onCancel: async () => {},
})

test('actual upstream runtime snapshots and no-op adapter swaps stay stable before, during, and after streaming', () => {
  const history = message('assistant', 'history', 'Synthetic history')
  const user = message('user', 'user', 'Synthetic prompt')
  const core = new IncrementalExternalStoreRuntimeCore(adapter(repository([history]), false))
  const runtime = new AssistantRuntimeImpl(core)
  let notifications = 0
  core.threads.getMainThreadRuntimeCore().subscribe(() => { notifications++ })
  const stages: Array<[ThreadMessage[], boolean]> = [
    [[history], false], [[history, user], true],
    [[history, user, message('assistant', 'reply', 'Synthetic')], true],
    [[history, user, message('assistant', 'reply', 'Synthetic stream')], true],
    [[history, user, message('assistant', 'reply', 'Synthetic stream')], false],
  ]
  for (const [messages, running] of stages) {
    const repo = repository(messages)
    core.setAdapter(adapter(repo, running))
    const before = notifications
    const snapshot = runtime.thread.getState()
    for (let render = 0; render < 60; render++) {
      assert.strictEqual(runtime.thread.getState(), snapshot, 'getSnapshot must not allocate on read')
      core.setAdapter(adapter(repo, running))
    }
    assert.equal(notifications, before, 'new adapter literals with unchanged state must remain silent')
  }
})

test('an actual upstream subscriber re-entering with the current streaming adapter cannot recurse', () => {
  const history = message('assistant', 'history', 'Synthetic history')
  let current = adapter(repository([history]), false)
  const core = new IncrementalExternalStoreRuntimeCore(current)
  let depth = 0
  core.threads.getMainThreadRuntimeCore().subscribe(() => {
    assert.ok(++depth < 10, 'a no-op subscriber must not create a render feedback loop')
    core.setAdapter({ ...current })
  })
  current = adapter(repository([history, message('user', 'user', 'Synthetic prompt')]), true)
  core.setAdapter(current)
  assert.equal(depth, 1)
})
