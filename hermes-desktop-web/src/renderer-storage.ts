/** Per-document Web Storage. Drafts and transcript tails never cross instances
 * or users or remain on disk in the shared Manager origin. */
export function createRendererStorage(maxBytes = 8 * 1024 * 1024): Storage {
  const values = new Map<string, string>()
  let bytes = 0
  return Object.freeze({
    get length() { return values.size },
    key(index: number) { return [...values.keys()][index] ?? null },
    getItem(key: string) { return values.get(String(key)) ?? null },
    removeItem(key: string) {
      key = String(key)
      const previous = values.get(key)
      if (previous !== undefined) bytes -= (key.length + previous.length) * 2
      values.delete(key)
    },
    clear() { values.clear(); bytes = 0 },
    setItem(key: string, value: string) {
      key = String(key); value = String(value)
      const previous = values.get(key)
      const nextBytes = bytes + (key.length + value.length) * 2 -
        (previous === undefined ? 0 : (key.length + previous.length) * 2)
      if (nextBytes > maxBytes) throw new DOMException('Renderer storage quota exceeded', 'QuotaExceededError')
      values.set(key, value); bytes = nextBytes
    }
  })
}

export function installRendererStorage(target: Pick<Window, 'localStorage' | 'sessionStorage' | 'addEventListener'>) {
  Object.defineProperties(target, {
    localStorage: { value: createRendererStorage(), configurable: false },
    sessionStorage: { value: createRendererStorage(), configurable: false }
  })
  // Electron's cross-window synchronization must not import other CM frames.
  target.addEventListener('storage', event => event.stopImmediatePropagation(), { capture: true })
}
