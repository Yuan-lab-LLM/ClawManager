import { createHash } from 'node:crypto'
import { mkdir, mkdtemp, readFile, rename, rm, writeFile } from 'node:fs/promises'
import path from 'node:path'

export function gitBlob(bytes) {
  return createHash('sha1').update(`blob ${bytes.length}\0`).update(bytes).digest('hex')
}

// Publish only a complete, verified tree. Files removed by an upstream upgrade
// must disappear too: Vite's plugin glob otherwise includes the old modules.
export async function materializeSourceCache(imported, entries, obtain) {
  const parent = path.dirname(imported)
  await mkdir(parent, { recursive: true })
  const staging = await mkdtemp(path.join(parent, '.source-stage-'))
  const prepared = path.join(staging, 'source')
  const previous = path.join(staging, 'previous')
  await mkdir(prepared)
  let next = 0
  let saved = false
  let published = false
  try {
    async function worker() {
      while (next < entries.length) {
        const [filename, expected] = entries[next++]
        if (path.isAbsolute(filename) || filename.includes('\\') ||
            filename.split('/').some(part => !part || part === '.' || part === '..')) {
          throw new Error('Unsafe cache entry')
        }
        let bytes
        try { bytes = await readFile(path.join(imported, filename)) } catch { /* No usable cache. */ }
        if (!bytes || gitBlob(bytes) !== expected) bytes = await obtain(filename, expected)
        if (!bytes || gitBlob(bytes) !== expected) throw new Error(`Hermes source integrity mismatch: ${filename}`)
        const target = path.join(prepared, filename)
        await mkdir(path.dirname(target), { recursive: true })
        await writeFile(target, bytes)
      }
    }
    // Settle every writer before removing a failed staging directory.
    const results = await Promise.allSettled(Array.from({ length: 8 }, () => worker()))
    const failed = results.find(result => result.status === 'rejected')
    if (failed) throw failed.reason
    try { await rename(imported, previous); saved = true } catch (error) {
      if (error.code !== 'ENOENT') throw error
    }
    try { await rename(prepared, imported); published = true } catch (error) {
      if (saved) { await rename(previous, imported); saved = false }
      throw error
    }
  } finally {
    // Keep the recoverable old tree if publication and rollback both failed.
    if (published || !saved) await rm(staging, { recursive: true, force: true })
  }
  return imported
}
