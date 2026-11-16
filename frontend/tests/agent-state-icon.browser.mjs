// Run on the test cluster: WORKSPACE_E2E_PACKAGE=/path/to/e2e/package.json node tests/agent-state-icon.browser.mjs
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { createRequire } from 'node:module';
import { createServer } from 'vite';
import react from '@vitejs/plugin-react';

const require = createRequire(process.env.WORKSPACE_E2E_PACKAGE || new URL('../../e2e/package.json', import.meta.url));
const { chromium } = require('playwright');
const root = path.resolve(import.meta.dirname, '..');
const types = ['openclaw', 'hermes', 'opencode', 'deepseek-harness', 'workbuddy', 'codex', 'claude-code'];
const states = ['running', 'stopped', 'error', 'auto_sleep'];
const entry = '/__agent-state-fixture.js';
const fixture = `
import React from 'react';
import {createRoot} from 'react-dom/client';
import AgentStateIcon from '/src/components/AgentStateIcon.tsx';
const types=${JSON.stringify(types)}, states=${JSON.stringify(states)};
createRoot(document.getElementById('root')).render(React.createElement('main', null,
  ...types.map(type=>React.createElement('section', {key:type,style:{display:'flex',gap:40,alignItems:'center',marginBottom:24}},
    React.createElement('span',{style:{width:140}},type),
    ...states.map(state=>React.createElement(AgentStateIcon,{key:state,type,state,label:type+' '+state,size:32})))),
  React.createElement(AgentStateIcon,{type:'openclaw',state:'creating',label:'transitional'}),
  React.createElement(AgentStateIcon,{type:'ubuntu',state:'running',label:'legacy'}),
  React.createElement(AgentStateIcon,{type:'codex',state:'sleeping',label:'sleep alias'}),
  React.createElement(AgentStateIcon,{type:'hermes',state:'running'}),
  React.createElement('div',{style:{height:1400}}),
  React.createElement(AgentStateIcon,{type:'codex',state:'running',label:'offscreen'})));
`;
let vite, browser;
try {
  vite = await createServer({ root, configFile: false, plugins: [react(), {
    name: 'agent-state-fixture',
    resolveId: id => id === entry ? entry : undefined,
    load: id => id === entry ? fixture : undefined,
    configureServer(server) {
      server.middlewares.use(async (request, response, next) => {
        if (request.url !== '/__agent-state-fixture.html') return next();
        response.setHeader('Content-Type', 'text/html');
        response.end(await server.transformIndexHtml(request.url, `<html><body style="padding:24px;font:14px sans-serif"><div id="root"></div><script type="module" src="${entry}"></script></body></html>`));
      });
    },
  }], server: { port: 0, host: '127.0.0.1', hmr: false }, logLevel: 'error' });
  await vite.listen();
  browser = await chromium.launch({ executablePath: process.env.CHROME_PATH || '/opt/google/chrome/chrome', headless: true, args: ['--no-sandbox'] });
  const page = await browser.newPage({ viewport: { width: 768, height: 800 } });
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(vite.resolvedUrls.local[0] + '__agent-state-fixture.html');
  const icon = label => page.getByRole('img', { name: label, exact: true });
  await icon('openclaw running').waitFor();
  await page.waitForFunction(() => document.querySelector('[aria-label="openclaw running"]').dataset.motionPaused === 'false');
  const style = locator => locator.evaluate(element => {
    const css = getComputedStyle(element);
    return { animation: css.animationName, duration: css.animationDuration, iterations: css.animationIterationCount, play: css.animationPlayState, opacity: css.opacity, filter: css.filter, transform: css.transform };
  });
  for (const type of types) {
    for (const state of states) {
      const node = icon(`${type} ${state}`);
      assert.equal(await node.getAttribute('data-agent-state'), state === 'auto_sleep' ? 'sleeping' : state);
      const image = node.locator('img');
      assert.equal(await image.getAttribute('src'), `/${type}.${type === 'deepseek-harness' ? 'svg' : 'png'}`);
      assert.equal(await image.evaluate(element => element.complete && element.naturalWidth > 0), true, `${type} native asset loads`);
    }
    const errorIcon = icon(`${type} error`);
    assert.deepEqual(await style(errorIcon.locator('.cm-agent-state-art')), { animation: 'none', duration: '0s', iterations: '1', play: 'running', opacity: '1', filter: 'none', transform: 'none' }, 'error preserves original art, color and position');
    assert.equal(await errorIcon.evaluate(element => getComputedStyle(element, '::before').content), 'none', 'error has no halo or red circle');
    const badge = await style(errorIcon.locator('.cm-agent-state-warning'));
    assert.notEqual(badge.animation, 'none');
    assert.equal(badge.duration, '2.2s');
    assert.equal(await icon(`${type} auto_sleep`).locator('.cm-agent-state-sleep-sign').count(), 1);
  }
  assert.equal(await icon('openclaw auto_sleep').locator('.cm-agent-state-sleep-eyes').count(), 1);
  assert.equal(await page.locator('.cm-agent-state-sleep-eyes').count(), 1, 'only OpenClaw receives closed eyes');
  assert.equal(await icon('sleep alias').getAttribute('data-agent-state'), 'sleeping');
  assert.equal(await icon('legacy').locator('svg').count(), 1, 'legacy type has monitor fallback');
  assert.equal(await icon('Hermes').count(), 1, 'default label uses existing type formatter');
  assert.equal(await icon('transitional').getAttribute('data-agent-state'), 'unknown');
  assert.equal((await style(icon('transitional').locator('.cm-agent-state-art'))).animation, 'none');
  assert.equal(await icon('transitional').locator('.cm-agent-state-warning').count(), 0, 'creating is never presented as an error');
  const runningArt = icon('openclaw running').locator('.cm-agent-state-art');
  const running = await style(runningArt);
  assert.notEqual(running.animation, 'none');
  assert.equal(running.iterations, 'infinite');
  const stoppedArt = icon('openclaw stopped').locator('.cm-agent-state-art');
  assert.equal((await style(stoppedArt)).iterations, '1');
  assert.equal((await style(stoppedArt)).duration, '0.7s');
  await page.waitForTimeout(800);
  assert.equal((await style(stoppedArt)).opacity, '0.32');
  assert.equal((await style(stoppedArt)).filter, 'grayscale(1)');
  const settled = (await style(stoppedArt)).transform;
  await page.waitForTimeout(120);
  assert.equal((await style(stoppedArt)).transform, settled, 'stopped icon remains still after power-down');
  const warning = icon('openclaw error').locator('.cm-agent-state-warning');
  const pulse = await warning.evaluate(async element => {
    const animation = element.getAnimations()[0];
    animation.pause();
    const samples = [];
    for (const time of [0, 308, 616]) {
      animation.currentTime = time;
      await new Promise(requestAnimationFrame);
      samples.push({ badge: getComputedStyle(element).transform,
        art: getComputedStyle(element.parentElement.querySelector('.cm-agent-state-art')).transform });
    }
    animation.play();
    return samples;
  });
  assert.equal(new Set(pulse.map(sample => sample.art)).size, 1, 'error artwork never shakes');
  assert.equal(new Set(pulse.map(sample => sample.badge)).size, 2, 'only error triangle scales up and back down');
  await page.waitForFunction(() => document.querySelector('[aria-label="offscreen"]').dataset.motionPaused === 'true');
  assert.equal((await style(icon('offscreen').locator('.cm-agent-state-art'))).play, 'paused');
  await icon('offscreen').scrollIntoViewIfNeeded();
  await page.waitForFunction(() => document.querySelector('[aria-label="offscreen"]').dataset.motionPaused === 'false');
  assert.equal((await style(icon('offscreen').locator('.cm-agent-state-art'))).play, 'running');
  await page.evaluate(() => { Object.defineProperty(document, 'hidden', { configurable: true, value: true }); document.dispatchEvent(new Event('visibilitychange')); });
  assert.equal((await style(icon('offscreen').locator('.cm-agent-state-art'))).play, 'paused', 'hidden document pauses animation');
  await page.evaluate(() => { Object.defineProperty(document, 'hidden', { configurable: true, value: false }); document.dispatchEvent(new Event('visibilitychange')); window.scrollTo(0, 0); });
  await page.emulateMedia({ reducedMotion: 'reduce' });
  for (const label of ['openclaw running', 'openclaw stopped', 'openclaw error', 'openclaw auto_sleep']) {
    assert.equal((await style(icon(label).locator('.cm-agent-state-art'))).animation, 'none');
  }
  assert.equal((await style(warning)).animation, 'none');
  assert.equal((await style(stoppedArt)).opacity, '0.32', 'reduced motion keeps final stopped appearance');
  if (process.env.AGENT_STATE_SCREENSHOTS) {
    await fs.mkdir(process.env.AGENT_STATE_SCREENSHOTS, { recursive: true });
    await page.screenshot({ path: path.join(process.env.AGENT_STATE_SCREENSHOTS, 'agent-state-icons.png') });
  }
  assert.deepEqual(errors, []);
  console.log('PASS seven native assets × four states; static error artwork/badge-only pulse; stopped settles; sleep eyes; unknown/fallback/a11y; offscreen/document pause; reduced motion');
} finally {
  await browser?.close();
  await vite?.close();
}
