import assert from 'node:assert/strict'
import test from 'node:test'
import { createRendererStorage, installRendererStorage } from '../src/renderer-storage.ts'

test('renderer state is isolated per document', () => {
  const a = createRendererStorage(), b = createRendererStorage()
  a.setItem('hermes.desktop.draft', 'instance A')
  assert.equal(a.getItem('hermes.desktop.draft'), 'instance A')
  assert.equal(b.getItem('hermes.desktop.draft'), null)
  a.clear()
  assert.equal(a.length, 0)
})

test('memory is bounded; replacing and deleting releases capacity', () => {
  const storage = createRendererStorage(12)
  storage.setItem('a', '12345')
  assert.throws(() => storage.setItem('b', '1'), { name: 'QuotaExceededError' })
  storage.setItem('a', '1')
  storage.setItem('b', '2')
  assert.equal(storage.length, 2)
  assert.equal(storage.key(0), 'a')
  storage.removeItem('a')
  assert.equal(storage.getItem('a'), null)
  assert.equal(storage.key(99), null)
})

test('installation never reads or changes Manager storage; storage events do not cross frames', () => {
  let handler: EventListener | undefined
  const manager = createRendererStorage()
  manager.setItem('auth', 'manager-owned')
  const target = {
    localStorage: manager, sessionStorage: manager,
    addEventListener: (_name: string, callback: EventListener) => { handler = callback }
  }
  installRendererStorage(target as unknown as Window)
  assert.equal(target.localStorage.getItem('auth'), null)
  assert.equal(manager.getItem('auth'), 'manager-owned')
  target.localStorage.setItem('draft', 'private')
  assert.equal(target.sessionStorage.getItem('draft'), null)
  let stopped = false
  handler?.({ stopImmediatePropagation() { stopped = true } } as Event)
  assert.equal(stopped, true)
})
