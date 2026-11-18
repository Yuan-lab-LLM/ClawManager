import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const layoutSource = readFileSync(
  path.resolve(scriptDir, "../src/components/UserLayout.tsx"),
  "utf8",
);
const routerSource = readFileSync(
  path.resolve(scriptDir, "../src/router/index.tsx"),
  "utf8",
);

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

assert(
  !layoutSource.includes("path: '/portal'") &&
    !layoutSource.includes('path: "/portal"'),
  "User sidebar must not expose Portal as a top-level navigation entry.",
);
assert(
  routerSource.includes('path="/portal"'),
  "Portal route must remain available for instance-page entry points.",
);

// Render the real user shell: Portal is discoverable from its account actions.
const { default: React } = await import('react');
const { renderToStaticMarkup } = await import('react-dom/server');
const { MemoryRouter } = await import('react-router-dom');
const { createServer } = await import('vite');
globalThis.localStorage = { getItem: () => null };
globalThis.window = { localStorage: globalThis.localStorage };
const server = await createServer({ server: { middlewareMode: true }, appType: 'custom' });
try {
  const { default: Layout } = await server.ssrLoadModule('/src/components/UserLayout.tsx');
  const { AuthProvider } = await server.ssrLoadModule('/src/contexts/AuthContext.tsx');
  const { I18nProvider } = await server.ssrLoadModule('/src/contexts/I18nContext.tsx');
  const html = renderToStaticMarkup(React.createElement(MemoryRouter, { initialEntries: ['/instances'] },
    React.createElement(AuthProvider, null, React.createElement(I18nProvider, null,
      React.createElement(Layout, { title: 'Instances' }, React.createElement('p', null, 'Instance list'))))));
  assert((html.match(/href="\/portal"/g) || []).length === 1, 'instance user shell retains a Portal link');
} finally { await server.close(); }
console.log('Portal remains available from instance account actions without a top-level sidebar entry.');
