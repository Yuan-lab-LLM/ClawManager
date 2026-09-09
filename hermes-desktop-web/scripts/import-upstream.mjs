import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
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
  const checkout = source ? path.resolve(source) : null
  const sourceBytes = new Map()
  if (checkout) {
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
  }
  const imported = path.join(root, '.upstream', 'source')
  let next = 0
  async function worker() {
    while (next < entries.length) {
      const [filename, expected] = entries[next++]
      const target = path.join(imported, filename)
      let cached
      try { cached = await readFile(target) } catch { /* Missing cache is downloaded below. */ }
      if (cached && gitBlob(cached) === expected) continue
      let bytes = sourceBytes.get(filename)
      if (!bytes) {
        for (let attempt = 0; attempt < 3; attempt++) {
          try {
            const response = await fetch(`https://raw.githubusercontent.com/NousResearch/hermes-agent/${lock.commit}/${filename}`, {
              signal: AbortSignal.timeout(30_000), redirect: 'error'
            })
            if (!response.ok) throw new Error(`Source download failed (${response.status}): ${filename}`)
            bytes = Buffer.from(await response.arrayBuffer())
            break
          } catch (error) { if (attempt === 2) throw error }
        }
      }
      if (!bytes) throw new Error(`Missing Hermes source: ${filename}`)
      const actualBlob = gitBlob(bytes)
      if (actualBlob !== expected) throw new Error(`Hermes source integrity mismatch: ${filename}`)
      await mkdir(path.dirname(target), { recursive: true })
      await writeFile(target, bytes)
    }
  }
  await Promise.all(Array.from({ length: 8 }, () => worker()))
  return imported
}

function gitBlob(bytes) {
  return createHash('sha1').update(`blob ${bytes.length}\0`).update(bytes).digest('hex')
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const sourceIndex = process.argv.indexOf('--source')
  await importUpstream(sourceIndex < 0 ? process.env.HERMES_UPSTREAM_SOURCE : process.argv[sourceIndex + 1])
  console.log(`Imported ${Object.keys(lock.files).length} verified Hermes source files at ${lock.commit}`)
}
