import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

export const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
export const lock = JSON.parse(await readFile(path.join(root, 'upstream.lock.json'), 'utf8'))

function git(cwd, args) {
  return execFileSync('git', ['-C', cwd, ...args], { maxBuffer: 256 * 1024 * 1024 })
}

export async function importUpstream(source) {
  if (lock.scope !== 'desktop-renderer' || lock.rendererEntry !== 'apps/desktop/src/main.tsx' ||
      !/^[a-f0-9]{40}$/.test(lock.commit) || !/^[a-f0-9]{40}$/.test(lock.sourceTree)) {
    throw new Error('A reviewed complete Desktop renderer lock is required')
  }
  const entries = Object.entries(lock.files)
  for (const [filename, expected] of entries) {
    if (filename.includes('\\') || filename.split('/').some(part => !part || part === '.' || part === '..') ||
        path.isAbsolute(filename) || !/^[A-Za-z0-9_./@-]+$/.test(filename) || !/^[a-f0-9]{40}$/.test(expected)) {
      throw new Error('Unsafe or invalid locked source entry')
    }
  }
  const imported = path.join(root, '.upstream', 'source')
  let checkout = source ? path.resolve(source) : null
  let temporaryRoot = null
  if (!checkout) {
    let cacheComplete = true
    for (const [filename, expected] of entries) {
      try {
        if (gitBlob(await readFile(path.join(imported, filename))) !== expected) cacheComplete = false
      } catch {
        cacheComplete = false
      }
      if (!cacheComplete) break
    }
    if (cacheComplete) return imported
    if (lock.repository !== 'https://github.com/NousResearch/hermes-agent.git' || !/^v[0-9]{4}\.[0-9]+\.[0-9]+$/.test(lock.ref)) {
      throw new Error('Unreviewed Hermes source repository or ref')
    }
    temporaryRoot = await mkdtemp(path.join(tmpdir(), 'clawmanager-hermes-source-'))
    checkout = path.join(temporaryRoot, 'source')
    let cloneError
    for (let attempt = 0; attempt < 3; attempt++) {
      try {
        execFileSync('git', [
          '-c', 'core.hooksPath=/dev/null', 'clone', '--no-checkout', '--depth', '1',
          '--single-branch', '--branch', lock.ref, lock.repository, checkout
        ], { maxBuffer: 256 * 1024 * 1024, stdio: ['ignore', 'pipe', 'pipe'] })
        cloneError = null
        break
      } catch (error) {
        cloneError = error
        await rm(checkout, { recursive: true, force: true })
      }
    }
    if (cloneError) {
      await rm(temporaryRoot, { recursive: true, force: true })
      throw new Error(`Hermes source clone failed: ${cloneError.message}`)
    }
  }
  const sourceBytes = new Map()
  try {
    const actual = git(checkout, ['rev-parse', `${lock.commit}^{commit}`]).toString().trim()
    if (actual !== lock.commit) throw new Error('Hermes source commit mismatch')
    if (git(checkout, ['rev-parse', `${lock.commit}^{tree}`]).toString().trim() !== lock.sourceTree) {
      throw new Error('Hermes source tree mismatch')
    }
    // Completeness is checked as well as individual content: selecting a few
    // widgets must never be mislabeled as the complete Desktop renderer.
    const tree = git(checkout, ['ls-tree', '-r', '-z', lock.commit, '--', ...lock.completeTrees]).toString()
    for (const entry of tree.split('\0').filter(Boolean)) {
      const match = /^100644 blob ([a-f0-9]{40})\t(.+)$/.exec(entry)
      if (!match || lock.files[match[2]] !== match[1]) throw new Error('Incomplete or changed Desktop source tree')
    }
    // One object read keeps 1,800+ pinned files fast, including binary fonts
    // and images. Never import mutable worktree bytes or execute Git hooks.
    const batch = execFileSync('git', ['-C', checkout, 'cat-file', '--batch'], {
      input: entries.map(([filename]) => `${lock.commit}:${filename}\n`).join(''),
      maxBuffer: 256 * 1024 * 1024
    })
    let offset = 0
    for (const [filename, expected] of entries) {
      const end = batch.indexOf(10, offset)
      if (end < 0) throw new Error('Truncated Git object header')
      const header = /^([a-f0-9]{40}) blob ([0-9]+)$/.exec(batch.subarray(offset, end).toString())
      if (!header || header[1] !== expected) throw new Error(`Git object mismatch: ${filename}`)
      const size = Number(header[2])
      offset = end + 1
      if (offset + size >= batch.length || batch[offset + size] !== 10) throw new Error('Truncated Git object')
      sourceBytes.set(filename, batch.subarray(offset, offset + size))
      offset += size + 1
    }
    if (offset !== batch.length) throw new Error('Unexpected trailing Git object data')
    let next = 0
    async function worker() {
      while (next < entries.length) {
        const [filename, expected] = entries[next++]
        const target = path.join(imported, filename)
        let cached
        try { cached = await readFile(target) } catch { /* Missing cache is imported below. */ }
        if (cached && gitBlob(cached) === expected) continue
        const bytes = sourceBytes.get(filename)
        if (!bytes) throw new Error(`Missing Hermes source: ${filename}`)
        const actualBlob = gitBlob(bytes)
        if (actualBlob !== expected) throw new Error(`Hermes source integrity mismatch: ${filename}`)
        await mkdir(path.dirname(target), { recursive: true })
        await writeFile(target, bytes)
      }
    }
    await Promise.all(Array.from({ length: 8 }, () => worker()))
    return imported
  } finally {
    if (temporaryRoot) await rm(temporaryRoot, { recursive: true, force: true })
  }
}

function gitBlob(bytes) {
  return createHash('sha1').update(`blob ${bytes.length}\0`).update(bytes).digest('hex')
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const sourceIndex = process.argv.indexOf('--source')
  await importUpstream(sourceIndex < 0 ? process.env.HERMES_UPSTREAM_SOURCE : process.argv[sourceIndex + 1])
  console.log(`Imported ${Object.keys(lock.files).length} verified Hermes source files at ${lock.commit}`)
}
