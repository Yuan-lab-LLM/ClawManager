import { readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const source = (relativePath) =>
  readFileSync(path.resolve(scriptDir, relativePath), 'utf8');

const versionComponent = source('../src/components/AppVersion.tsx');
const versionService = source('../src/services/versionService.ts');

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

assert(
  /api\s*\.get\('\/version'\)/.test(versionService),
  'Version service must read the running backend build information.',
);
assert(
  versionComponent.includes('data-testid="clawmanager-version"') &&
    versionComponent.includes('buildInfo?.version'),
  'Version component must render the running ClawManager version.',
);
// The shared shell owns version rendering. Exercise both real layout trees.
const { default: React } = await import('react');
const { renderToStaticMarkup } = await import('react-dom/server');
const { MemoryRouter } = await import('react-router-dom');
const { createServer } = await import('vite');
globalThis.localStorage = { getItem: () => null };
globalThis.window = { localStorage: globalThis.localStorage };
const server = await createServer({ server: { middlewareMode: true }, appType: 'custom' });
try {
  const { AuthProvider } = await server.ssrLoadModule('/src/contexts/AuthContext.tsx');
  const { I18nProvider } = await server.ssrLoadModule('/src/contexts/I18nContext.tsx');
  for (const file of ['UserLayout', 'AdminLayout']) {
    const { default: Layout } = await server.ssrLoadModule(`/src/components/${file}.tsx`);
    const html = renderToStaticMarkup(React.createElement(MemoryRouter, null,
      React.createElement(AuthProvider, null, React.createElement(I18nProvider, null,
        React.createElement(Layout, { title: 'Workspace' }, React.createElement('p', null, 'Content'))))));
    assert((html.match(/data-testid="clawmanager-version"/g) || []).length === 1, `${file} exposes exactly one shared version badge`);
    assert(html.indexOf('ClawManager logo') < html.indexOf('data-testid="clawmanager-version"'), 'version follows the brand');
  }
} finally { await server.close(); }
console.log('ClawManager version service/component and both shared layout trees are valid.');
