import { defineConfig, type Plugin } from 'vite'
import babel from '@rolldown/plugin-babel'
import react, { reactCompilerPreset } from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import postcss from 'postcss'
import { builtinModules, createRequire } from 'node:module'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { webAdaptations } from './scripts/web-adaptations.mjs'
import { dependencyLicenseFile, thirdPartyNotices } from './scripts/third-party-notices.mjs'

const root = path.dirname(fileURLToPath(import.meta.url))
const imported = path.join(root, '.upstream/source')
const shared = path.join(imported, 'apps/shared/src')
const requireFromApp = createRequire(path.join(root, 'package.json'))
const lock = JSON.parse(readFileSync(path.join(root, 'upstream.lock.json'), 'utf8')) as {
  files: Record<string, string>; rendererEntry: string
}
const normalize = (filename: string) => filename.replaceAll('\\', '/')
const importedPrefix = normalize(imported) + '/'
const hostOnly = new Set([...builtinModules, ...builtinModules.map(name => `node:${name}`), 'electron', 'node-pty', 'simple-git', 'get-windows'])

function lockedBrowserGraph(): Plugin {
  return {
    name: 'clawmanager:locked-desktop-browser-graph',
    enforce: 'pre',
    resolveId(id) {
      if (hostOnly.has(id) || id.startsWith('node:')) {
        throw new Error(`Host-only dependency reached the browser renderer: ${id}`)
      }
      // The publisher package's MIT identifier does not grant redistribution
      // of its separately licensed commercial fonts. The exact CSS adaptation
      // removes Collapse; fail closed if any such asset is reintroduced.
      if (normalize(id).includes('@nous-research/ui/') && normalize(id).includes('/fonts/')) {
        throw new Error('Uncleared @nous-research/ui font reached the browser bundle')
      }
      return null
    },
    load(id) {
      const filename = normalize(id.split('?')[0])
      if (!filename.startsWith(importedPrefix)) return null
      const relative = filename.slice(importedPrefix.length)
      const expected = lock.files[relative]
      if (!expected) throw new Error(`Unlocked upstream module reached the build: ${relative}`)
      const bytes = readFileSync(filename)
      const blob = createHash('sha1').update(`blob ${bytes.length}\0`).update(bytes).digest('hex')
      if (blob !== expected) throw new Error(`Upstream module was changed after import: ${relative}`)
      return null
    },
    transform(code, id) {
      if (normalize(id.split('?')[0]) !== importedPrefix + 'apps/desktop/src/styles.css') return null
      const anchor = "@import 'tailwindcss';"
      if (code.split(anchor).length !== 2) throw new Error('Locked Desktop Tailwind import anchor changed')
      // .upstream is gitignored. Tailwind's automatic CM-root scan therefore
      // misses the real renderer. Explicit sources opt the complete verified
      // tree in; source(none) avoids accidental classes from unrelated files.
      // This build-only annotation never changes the cached Git blob bytes.
      const managedSource = normalize(path.relative(path.join(imported, 'apps/desktop/src'), path.join(root, 'src')))
      return { code: code.replace(anchor, `@import 'tailwindcss' source(none);\n@source "./";\n@source ${JSON.stringify(managedSource)};`), map: null }
    },
    generateBundle(_options, bundle) {
      const entry = normalize(path.join(imported, lock.rendererEntry))
      if (![...this.getModuleIds()].some(id => normalize(id.split('?')[0]) === entry)) {
        throw new Error('The complete upstream Desktop main entry is missing; a widget-only build is not a Desktop renderer')
      }
      // A dynamic import in source is insufficient if chunk merging hoists
      // Desktop's module-level bridge calls into the bootstrap's static graph.
      // Shared pure protocol helpers and vendor libraries are allowed; every
      // Desktop module must remain behind the installed browser bridge.
      const pending = Object.values(bundle).filter(item => item.type === 'chunk' && item.isEntry).map(item => item.fileName)
      const visited = new Set<string>()
      while (pending.length) {
        const filename = pending.pop()!
        if (visited.has(filename)) continue
        visited.add(filename)
        const chunk = bundle[filename]
        if (!chunk || chunk.type !== 'chunk') continue
        for (const id of Object.keys(chunk.modules)) {
          if (normalize(id).startsWith(importedPrefix + 'apps/desktop/src/')) {
            throw new Error(`Desktop module executes before browser bridge installation: ${normalize(id).slice(importedPrefix.length)}`)
          }
        }
        pending.push(...chunk.imports)
      }
      // Do not let a successful JS build ship an unstyled full renderer.
      // Parse selectors/declarations so merged CSS rules cannot fool this gate.
      const required = new Map([
        ['.min-h-0', 'min-height'], ['.min-w-0', 'min-width'],
        ['.overflow-hidden', 'overflow'], ['.shrink-0', 'flex-shrink'],
        ['.w-full', 'width'], ['.overscroll-contain', 'overscroll-behavior'],
        ['.grid-cols-2', 'grid-template-columns'], ['.truncate', 'text-overflow'],
        [String.raw`.contain-\[layout_paint\]`, 'contain'],
        [String.raw`.bg-\(--ui-chat-surface-background\)`, 'background-color']
      ])
      for (const asset of Object.values(bundle)) {
        if (/\b(?:Collapse|RulesCompressed|RulesExpanded|Neuebit|Mondwest)[^/]*\.(?:woff2?|ttf|otf)$/i.test(asset.fileName)) {
          throw new Error(`Uncleared commercial font reached the output: ${asset.fileName}`)
        }
        if (asset.type !== 'asset' || !asset.fileName.endsWith('.css')) continue
        const css = typeof asset.source === 'string' ? asset.source : Buffer.from(asset.source).toString()
        postcss.parse(css).walkRules(rule => {
          for (const selector of rule.selector.split(',')) {
            const name = selector.trim()
            const property = required.get(name)
            if (property && rule.nodes.some(node => node.type === 'decl' && node.prop === property)) required.delete(name)
          }
        })
      }
      if (required.size) throw new Error(`Desktop Tailwind source coverage is incomplete: ${[...required.keys()].join(', ')}`)
    }
  }
}

function localEmojiAssets(): Plugin {
  return {
    name: 'clawmanager:desktop-local-emoji-assets',
    generateBundle() {
      const packageDir = path.dirname(requireFromApp.resolve('emojibase-data/package.json'))
      for (const relative of ['en/data.json', 'en/messages.json', 'en/shortcodes/emojibase.json']) {
        this.emitFile({ type: 'asset', fileName: `emojibase/${relative}`, source: readFileSync(path.join(packageDir, relative)) })
      }
    }
  }
}

function compilerPreset() {
  // Same compiler/code filter as the locked Desktop config. Validate the
  // optional plugin API rather than carrying its unchecked filter dereference
  // into the CM build config (the original file remains byte-locked).
  const preset = reactCompilerPreset()
  if (!preset.rolldown.filter) throw new Error('Pinned React compiler filter API is unavailable')
  preset.rolldown.filter.code = /\/>|<\/|from\s*['"][^'"]*react/
  return preset
}

export default defineConfig(() => {
  const desktop = path.join(imported, 'apps/desktop/src')
  const driverDir = path.dirname(requireFromApp.resolve('driver.js'))
  return {
    root,
    base: './',
    publicDir: path.join(imported, 'apps/desktop/public'),
    // Docker invokes the build script from /app, not the package directory.
    // Babel resolves the compiler preset's plugin names independently of Vite.
    plugins: [webAdaptations(), lockedBrowserGraph(), react(), babel({ cwd: root, presets: [compilerPreset()] }), tailwindcss(), localEmojiAssets(), thirdPartyNotices(root)],
    css: { postcss: { plugins: [] } },
    optimizeDeps: { exclude: ['driver.js', 'driver.js/dist/driver.js.iife.js', 'driver.js/dist/driver.js.iife.js?raw', 'driver.js/dist/driver.css?raw'] },
    resolve: {
      alias: [
        { find: '@/debug/dev-only', replacement: path.join(desktop, 'debug/dev-only.noop.ts') },
        { find: '@hermes/plugin-sdk', replacement: path.join(desktop, 'sdk/index.ts') },
        { find: /^@hermes\/shared$/, replacement: path.join(shared, 'index.ts') },
        { find: '@hermes/shared/billing', replacement: path.join(shared, 'billing-types.ts') },
        ...['billing-policy', 'charge-settlement', 'skin', 'translucency'].map(name => ({ find: `@hermes/shared/${name}`, replacement: path.join(shared, `${name}.ts`) })),
        { find: 'driver.js/dist/driver.js.iife.js?raw', replacement: path.join(driverDir, 'driver.js.iife.js') + '?raw' },
        { find: 'driver.js/dist/driver.js.iife.js', replacement: path.join(driverDir, 'driver.js.iife.js') },
        { find: '@', replacement: desktop }
      ],
      dedupe: ['react', 'react-dom', 'react-router', '@tanstack/react-query']
    },
    // The six reviewed upstream split groups keep renderer contexts singular
    // and expensive syntax/diagram engines lazy. Do not replace the full App
    // with a small entry merely to reduce the resulting artifact size.
    build: {
      license: { fileName: dependencyLicenseFile },
      sourcemap: false, target: 'es2023', outDir: path.join(root, 'dist'), chunkSizeWarningLimit: 25000,
      rolldownOptions: { output: { advancedChunks: { groups: [
        { name: 'vendor-react', test: /node_modules[\\/](react|react-dom|scheduler|react-router|@tanstack[\\/]react-query)[\\/]/ },
        { name: 'vendor-md', test: /node_modules[\\/](property-information|hast-util-[^\\/]+|mdast-util-[^\\/]+|micromark[^\\/]*|unist-util-[^\\/]+|vfile[^\\/]*|unified|stringify-entities|space-separated-tokens|comma-separated-tokens|zwitch|html-void-elements|devlop|style-to-js|style-to-object|clsx)[\\/]/ },
        { name: 'vendor-util', test: /node_modules[\\/](lodash-es|es-toolkit|uuid|dayjs|d3-array|d3-color|d3-force|d3-interpolate|d3-time[^\\/]*|dompurify|stylis)[\\/]/ },
        { name: 'mermaid', test: /node_modules[\\/](mermaid|cytoscape|dagre|khroma|elkjs|d3|d3-[^\\/]+|@mermaid-js)[\\/]/ },
        { name: 'shiki', test: /node_modules[\\/](shiki|@shikijs|react-shiki|@streamdown[\\/]code|oniguruma-to-es|oniguruma-parser|regex(-[^\\/]+)?)[\\/]/ },
        { name: 'katex', test: /node_modules[\\/]katex[\\/]/ }
      ] } } }
    }
  }
})
