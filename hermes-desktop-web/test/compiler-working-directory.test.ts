import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'
import { transformAsync } from '@babel/core'

const root = fileURLToPath(new URL('../', import.meta.url))

test('compiler resolution is anchored to the renderer package, not the caller working directory', async () => {
  const config = readFileSync(path.join(root, 'vite.config.ts'), 'utf8')
  assert.match(config, /babel\(\{\s*cwd:\s*root,\s*presets:\s*\[compilerPreset\(\)\]/)
  const previousDirectory = process.cwd()
  try {
    process.chdir(path.dirname(root.replace(/[\\/]$/, '')))
    const result = await transformAsync('export function TestCard({ value }) { return <span>{value}</span> }', {
      cwd: root,
      filename: path.join(root, 'src/compiler-cwd-fixture.jsx'),
      configFile: false,
      babelrc: false,
      plugins: ['babel-plugin-react-compiler'],
      parserOpts: { plugins: ['jsx'] },
    })
    assert.match(result?.code ?? '', /react\/compiler-runtime/)
  } finally {
    process.chdir(previousDirectory)
  }
})
