import { createHash } from 'node:crypto'
import { cp, mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { importUpstream, lock, root } from './import-upstream.mjs'

async function buildInputDigest() {
  const files = ['index.html', 'package.json', 'package-lock.json', 'upstream.lock.json', 'vite.config.ts', 'tsconfig.json']
  async function walk(directory) {
    for (const entry of await readdir(path.join(root, directory), { withFileTypes: true })) {
      const relative = `${directory}/${entry.name}`
      if (entry.isDirectory()) await walk(relative)
      else if (entry.isFile()) files.push(relative)
      else throw new Error(`Unexpected non-file build input: ${relative}`)
    }
  }
  await walk('src')
  await walk('scripts')
  const hash = createHash('sha256')
  for (const filename of files.sort()) hash.update(filename).update('\0').update(await readFile(path.join(root, filename))).update('\0')
  return hash.digest('hex')
}

// Runtime packages must resolve to the exact verified monorepo lock, including
// transitive/peer edges. Exact direct versions alone do not pin React's stores.
// Hoisting is not identity: compare each consumer's resolved package version
// and tarball integrity, not node_modules directory layouts.
async function auditDependencies(imported) {
  const readJSON = async filename => JSON.parse(await readFile(filename, 'utf8'))
  const upstream = await readJSON(path.join(imported, 'package-lock.json'))
  const current = await readJSON(path.join(root, 'package-lock.json'))
  const manifest = await readJSON(path.join(root, 'package.json'))
  const desktop = await readJSON(path.join(imported, 'apps/desktop/package.json'))
  const shared = await readJSON(path.join(imported, 'apps/shared/package.json'))
  // These are upstream-declared dependencies used only by the CSS/build
  // pipeline. They remain exact direct pins and are audited separately below.
  const buildOnly = new Set(['@tailwindcss/typography', '@tailwindcss/vite', 'tailwindcss'])
  const sourceOrHost = new Set(['@hermes/shared', 'node-pty', 'simple-git'])
  function resolve(packages, from, name) {
    for (let at = from; ; at = path.posix.dirname(at)) {
      const key = `${at && at !== '.' ? `${at}/` : ''}node_modules/${name}`
      if (packages[key]) return key
      if (!at || at === '.') return null
    }
  }
  const expectedRoots = Object.keys(desktop.dependencies).filter(name => !sourceOrHost.has(name)).sort()
  if (JSON.stringify(expectedRoots) !== JSON.stringify(Object.keys(manifest.dependencies).sort())) {
    throw new Error('Desktop dependency roots differ from the reviewed browser/host boundary')
  }
  const checked = new Set(), identities = [], errors = []
  async function visit(upKey, localKey, chain) {
    const key = `${upKey}|${localKey}`
    if (checked.has(key)) return
    checked.add(key)
    const up = upstream.packages[upKey], local = current.packages[localKey]
    if (!up || !local) { errors.push(`${chain}: missing required package`); return }
    if (up.version !== local.version || !up.integrity || up.integrity !== local.integrity) {
      errors.push(`${chain}: expected ${up.version} and its upstream integrity, got ${local.version}`)
    }
    try {
      const installed = await readJSON(path.join(root, localKey, 'package.json'))
      if (installed.version !== local.version) errors.push(`${chain}: installed package does not match CM lock`)
    } catch { errors.push(`${chain}: required installed package is missing`) }
    identities.push([chain, up.version, up.integrity])
    const dependencies = new Set([
      ...Object.keys(up.dependencies || {}), ...Object.keys(up.optionalDependencies || {}),
      ...Object.keys(up.peerDependencies || {})
    ])
    for (const name of [...dependencies].sort()) {
      const a = resolve(upstream.packages, upKey, name)
      const b = resolve(current.packages, localKey, name)
      const optional = Object.hasOwn(up.optionalDependencies || {}, name) || up.peerDependenciesMeta?.[name]?.optional
      if ((!a || !b) && optional) continue
      await visit(a, b, `${chain}>${name}`)
    }
  }
  for (const name of expectedRoots) {
    if (manifest.dependencies[name] !== desktop.dependencies[name]) errors.push(`${name}: direct pin differs from upstream`)
    if (!buildOnly.has(name)) {
      await visit(resolve(upstream.packages, 'apps/desktop', name), resolve(current.packages, '', name), name)
    }
  }
  for (const name of Object.keys(shared.dependencies || {})) {
    await visit(resolve(upstream.packages, 'apps/shared', name), resolve(current.packages, '', name), `@hermes/shared>${name}`)
  }
  if (errors.length) throw new Error(`Desktop dependency closure drift:\n${errors.join('\n')}`)

  // Do not call build-tool drift runtime equivalence, or pull Electron's
  // optional peers in merely because the full monorepo happened to install
  // them. Record actual matched tool edges separately for review. Foreign
  // OS/CPU optional bindings are not renderer dependencies.
  const toolSeen = new Set(), toolDifferences = new Map()
  function visitTool(upKey, localKey) {
    const key = `${upKey}|${localKey}`
    if (toolSeen.has(key)) return
    toolSeen.add(key)
    const up = upstream.packages[upKey], local = current.packages[localKey]
    if (!up || !local) return
    if (up.version !== local.version) {
      const name = upKey.split('node_modules/').at(-1)
      toolDifferences.set(`${name}@${up.version}:${local.version}`, { package: name, upstream: up.version, current: local.version })
    }
    for (const name of new Set([...Object.keys(up.dependencies || {}), ...Object.keys(up.peerDependencies || {})])) {
      const a = resolve(upstream.packages, upKey, name), b = resolve(current.packages, localKey, name)
      if (a && b) visitTool(a, b)
    }
  }
  for (const name of new Set([...buildOnly, ...Object.keys(manifest.devDependencies || {})])) {
    visitTool(resolve(upstream.packages, 'apps/desktop', name), resolve(current.packages, '', name))
  }
  return {
    policy: 'exact-upstream-runtime-version-and-integrity',
    roots: expectedRoots.filter(name => !buildOnly.has(name)).length,
    resolved_nodes: checked.size,
    closure_sha256: createHash('sha256').update(JSON.stringify(identities)).digest('hex'),
    build_only_roots: [...buildOnly],
    build_toolchain_version_differences: [...toolDifferences.values()]
  }
}

const sourceIndex = process.argv.indexOf('--source')
const imported = await importUpstream(sourceIndex < 0 ? process.env.HERMES_UPSTREAM_SOURCE : process.argv[sourceIndex + 1])
const dependencyAudit = await auditDependencies(imported)
console.log(`Locked renderer dependency closure: ${dependencyAudit.resolved_nodes} resolved nodes, ${dependencyAudit.closure_sha256}`)
if (process.argv.includes('--audit-dependencies')) {
  console.log(JSON.stringify(dependencyAudit, null, 2))
} else {
const inputDigest = await buildInputDigest()
const { build } = await import('vite')
await build({ configFile: path.join(root, 'vite.config.ts'), root })
if (await buildInputDigest() !== inputDigest) throw new Error('Renderer build inputs changed during compilation; rebuild before publishing the artifact')
const output = path.resolve(root, '../frontend/public/hermes-desktop-web')
const expectedOutput = path.join(path.dirname(root), 'frontend', 'public', 'hermes-desktop-web')
if (output !== expectedOutput || path.basename(output) !== 'hermes-desktop-web') throw new Error('Unsafe output path')
// Only this generated directory is replaced; stale hashed chunks must not ship.
await rm(output, { recursive: true, force: true })
await mkdir(output, { recursive: true })
await cp(path.join(root, 'dist'), output, { recursive: true })
await cp(path.join(imported, 'LICENSE'), path.join(output, 'HERMES-LICENSE.txt'))
await writeFile(path.join(output, 'build-info.json'), JSON.stringify({
  bridge_version: '1', scope: 'desktop-renderer', acceptance: 'not-asserted-by-build', hermes_ref: lock.ref,
  hermes_commit: lock.commit, desktop_package_version: lock.desktopPackageVersion,
  renderer_entry: lock.rendererEntry, upstream_tree: lock.sourceTree,
  dependency_audit: dependencyAudit,
  build_input_sha256: inputDigest,
  tailwind_sources: ['apps/desktop/src', 'clawmanager/hermes-desktop-web/src'],
  source_files: lock.files
}, null, 2) + '\n')
console.log(`Locked Hermes Desktop renderer built (runtime/browser acceptance still required): ${output}`)
}
