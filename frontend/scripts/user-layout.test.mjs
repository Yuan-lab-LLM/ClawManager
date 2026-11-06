import assert from 'node:assert/strict';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import { createServer } from 'vite';

// The real providers read browser storage during their initial render.
globalThis.localStorage = { getItem: () => null };
globalThis.window = { localStorage: globalThis.localStorage };
const server = await createServer({ server: { middlewareMode: true }, appType: 'custom' });
try {
  const { default: UserLayout } = await server.ssrLoadModule('/src/components/UserLayout.tsx');
  const { AuthProvider } = await server.ssrLoadModule('/src/contexts/AuthContext.tsx');
  const { I18nProvider } = await server.ssrLoadModule('/src/contexts/I18nContext.tsx');
  for (const props of [{}, { fillHeight: true }, { fillHeight: true, scrollableMain: true }]) {
    let childRenders = 0, accessoryRenders = 0;
    const Child = () => { childRenders++; return React.createElement('iframe', { title: 'native instance' }); };
    const Accessory = () => { accessoryRenders++; return React.createElement('button', null, 'Title action'); };
    const html = renderToStaticMarkup(React.createElement(MemoryRouter, null,
      React.createElement(AuthProvider, null, React.createElement(I18nProvider, null,
        React.createElement(UserLayout, { ...props, title: 'Workspace title', titleAccessory: React.createElement(Accessory) }, React.createElement(Child))))));
    assert.equal(childRenders, 1, 'Responsive layouts must render the live child only once');
    assert.equal((html.match(/<iframe\b/g) || []).length, 1, 'Only one native iframe may start');
    assert.equal(accessoryRenders, 1, 'Title actions must render only once');
    assert.equal((html.match(/<main\b/g) || []).length, 1, 'There must be one main landmark');
  }
  console.log('UserLayout renders one live child, iframe, title action and main in all height modes.');
} finally { await server.close(); }
