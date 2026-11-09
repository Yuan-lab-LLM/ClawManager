import assert from 'node:assert/strict'
import { mkdtemp, mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { gitBlob, materializeSourceCache } from '../scripts/source-cache.mjs'

test('upgrade removes retired plugins and reuses only verified cached bytes', async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), 'hermes-source-cache-'))
  try {
    const cache = path.join(root, 'source')
    await mkdir(path.join(cache, 'plugins/accent'), { recursive: true })
    await writeFile(path.join(cache, 'plugins/accent/plugin.tsx'), 'retired')
    await writeFile(path.join(cache, 'keep.ts'), 'verified')
    await writeFile(path.join(cache, 'update.ts'), 'old')
    const fetched: string[] = []
    const entries = [['keep.ts', gitBlob(Buffer.from('verified'))], ['update.ts', gitBlob(Buffer.from('new'))]]
    await materializeSourceCache(cache, entries, async (name: string) => { fetched.push(name); return Buffer.from('new') })
    assert.deepEqual(fetched, ['update.ts'])
    assert.deepEqual((await readdir(cache)).sort(), ['keep.ts', 'update.ts'])
    assert.equal(await readFile(path.join(cache, 'update.ts'), 'utf8'), 'new')
  } finally { await rm(root, { recursive: true, force: true }) }
})

test('failed source verification leaves the previous cache intact', async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), 'hermes-source-cache-'))
  try {
    const cache = path.join(root, 'source')
    await mkdir(cache)
    await writeFile(path.join(cache, 'old.ts'), 'previous')
    await assert.rejects(materializeSourceCache(cache, [['new.ts', gitBlob(Buffer.from('expected'))]], async () => Buffer.from('corrupt')), /integrity mismatch/)
    assert.equal(await readFile(path.join(cache, 'old.ts'), 'utf8'), 'previous')
    assert.deepEqual(await readdir(root), ['source'])
  } finally { await rm(root, { recursive: true, force: true }) }
})
