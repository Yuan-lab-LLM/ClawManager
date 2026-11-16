import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { createRequire } from 'node:module';
const { chromium } = createRequire(
  new URL('../../e2e/package.json', import.meta.url),
)('playwright');
const base = process.env.ADMIN_OVERVIEW_URL || 'http://127.0.0.1:9031',
  out =
    process.env.ADMIN_OVERVIEW_ARTIFACTS || '/tmp/cm-admin-overview-browser';
await fs.mkdir(out, { recursive: true });
const now = new Date().toISOString(),
  start = '2026-11-15T16:00:00Z',
  modelValues = [1000, 700, 500, 300, 200, 100, 50, 25],
  models = modelValues.map((tokens, i) => ({
    name:
      i === 0
        ? 'deepseek-v4.1-flash-with-a-very-long-provider-model-name'
        : 'model-' + i,
    tokens,
  }));
const types = ['openclaw', 'hermes', 'opencode', 'deepseek-harness'].map(
  (type, i) => ({
    type,
    instances: [100, 50, 147, 73][i],
    runtime_pods: [292, 219, 147, 73][i],
    ready_pods: [80, 56, 37, 19][i],
  }),
);
const coverage = Object.fromEntries(
  ['instances', 'usage', 'costs', 'conversations', 'runtimes'].map((key) => [
    key,
    { status: key === 'conversations' ? 'partial' : 'ready' },
  ]),
);
const normal = {
  timezone: 'Asia/Shanghai',
  window: { start, end: now },
  total_users: 1036,
  total_instances: 370,
  active_users: 121,
  active_instances: 237,
  idle_instances: 35,
  idle_total: 370,
  usage: {
    total_tokens: 2875,
    conversations: 1248,
    success_rate: 99.5,
    average_response_ms: 17472,
    currencies: [
      { currency: 'USD', amount: 1732.54, complete: true },
      { currency: 'EUR', amount: 90, complete: false },
    ],
  },
  history: Array.from({ length: 19 }, (_, i) => ({
    at: new Date(Date.parse(start) + i * 3600000).toISOString(),
    tokens: i === 0 ? 0 : i * 50,
    conversations: i * 3,
  })),
  models,
  types,
  coverage,
};
const resource = (used, capacity, requested, allocatable = capacity) => ({
  used,
  capacity,
  requested,
  allocatable,
  unit: 'GiB',
  observed_at: now,
});
const cluster = {
  node_count: 12,
  ready_nodes: 12,
  cpu: resource(11.5, 96, 100.8),
  memory: resource(90.6, 503.5, 63.4, 503.4),
  disk: resource(738.5, 3516.7, 190, 3165.1),
  nodes: Array.from({ length: 12 }, (_, i) => ({
    name: i ? 'worker-' + i : 'k8s-master',
    ready: i !== 11,
    roles: [i ? 'worker' : 'control-plane'],
    internal_ip: '10.130.81.' + (54 + i),
    pod_count: 30 + i,
    kubelet_version: 'v1.35.0',
    cpu: resource(1, 8, 2),
    memory: resource(5, 42, 8),
    disk: resource(20, 300, 0),
  })),
};
let mode = 'normal',
  locale = 'zh',
  reads = 0,
  nodeCpu = 1;
const errors = [],
  checks = [];
const browser = await chromium.launch({ channel: 'chrome', headless: true });
try {
  const page = await browser.newPage({
    viewport: { width: 1440, height: 900 },
    reducedMotion: 'reduce',
  });
  page.setDefaultTimeout(10000);
  page.on('pageerror', (error) => errors.push(error.message));
  await page.addInitScript(() => {
    localStorage.setItem('access_token', 'fixture');
    if (!localStorage.getItem('clawmanager_locale'))
      localStorage.setItem('clawmanager_locale', 'zh');
  });
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url());
    let data = {};
    if (mode === 'loading' && url.pathname.endsWith('/admin/dashboard'))
      await new Promise((r) => setTimeout(r, 1800));
    if (url.pathname.endsWith('/auth/me'))
      data = { id: 1, username: 'admin', role: 'admin' };
    else if (url.pathname.endsWith('/version'))
      data = { version: 'test', commit: 'test' };
    else if (url.pathname.endsWith('/admin/dashboard')) {
      reads++;
      if (mode === 'error') {
        await route.fulfill({ status: 503, json: { error: 'unavailable' } });
        return;
      }
      data = structuredClone(normal);
      if (mode === 'runtime-error') {
        data.coverage.runtimes = { status: 'unavailable' };
        data.types = data.types.map((row) => ({
          ...row,
          runtime_pods: 0,
          ready_pods: 0,
        }));
      }
      if (mode === 'empty') {
        data.total_users =
          data.total_instances =
          data.active_users =
          data.active_instances =
          data.idle_instances =
          data.idle_total =
            0;
        data.usage = {
          total_tokens: 0,
          conversations: 0,
          success_rate: null,
          average_response_ms: null,
          currencies: [],
        };
        data.models = [];
        data.types = data.types.map((row) => ({
          ...row,
          instances: 0,
          runtime_pods: 0,
          ready_pods: 0,
        }));
        data.history = data.history.map((row) => ({
          ...row,
          tokens: 0,
          conversations: 0,
        }));
      }
      if (mode === 'missing') {
        data.usage = {
          total_tokens: null,
          conversations: null,
          success_rate: null,
          average_response_ms: null,
          currencies: [],
        };
        data.history = data.history.map((row) => ({
          ...row,
          tokens: null,
          conversations: null,
        }));
        data.models = [];
        data.coverage.usage = { status: 'unavailable' };
        data.coverage.conversations = { status: 'unavailable' };
      }
    } else if (url.pathname.endsWith('/cluster-resources')) {
      data = structuredClone(cluster);
      for (const node of data.nodes) node.cpu.used = nodeCpu;
      if (mode === 'missing') {
        for (const key of ['cpu', 'memory', 'disk']) data[key].used = null;
      }
      if (mode === 'empty') {
        data.node_count = data.ready_nodes = 0;
        data.nodes = [];
        for (const key of ['cpu', 'memory', 'disk'])
          data[key] = {
            used: null,
            capacity: 0,
            allocatable: 0,
            requested: 0,
            unit: 'GiB',
          };
      }
    }
    await route.fulfill({ json: { data } });
  });
  const load = async () => {
    await page.goto(base + '/admin', { waitUntil: 'domcontentloaded' });
    await page.locator('.ao-overview').waitFor();
    if (mode !== 'loading')
      await page.waitForFunction(
        () =>
          document.querySelector('.ao-dashboard')?.getAttribute('aria-busy') ===
          'false',
      );
  };
  await load();
  assert.equal(await page.locator('.ao-overview>.ao-panel').count(), 2);
  assert.equal(await page.locator('.ao-metrics-primary>article').count(), 4);
  assert.equal(await page.locator('.ao-metrics-health>article').count(), 4);
  assert.equal(await page.locator('.ao-type-pies .ao-pie').count(), 2);
  assert.equal(await page.locator('.ao-model-content .ao-pie').count(), 1);
  assert.equal(await page.getByText(/Session|质量与治理/).count(), 0);
  assert.match(
    await page.locator('.ao-gauge-readings').first().innerText(),
    /105%/,
  );
  checks.push(
    'approved groups/pies/no Session/no governance/uncapped requests',
  );
  mode = 'runtime-error';
  await load();
  assert.equal(
    await page
      .locator('.ao-type-table-row')
      .first()
      .locator('span')
      .nth(1)
      .innerText(),
    '— —',
  );
  assert.match(
    await page.locator('.ao-type-table-row').first().innerText(),
    /100/,
  );
  assert.equal(await page.locator('.ao-type-pies .ao-pie').count(), 1);
  mode = 'normal';
  await load();
  checks.push(
    'Runtime outage preserves instance distribution and uses unknown instead of zero',
  );
  await page
    .getByRole('combobox', { name: '费用币种', exact: true })
    .selectOption('EUR');
  assert.match(await page.locator('.ao-metrics-primary').innerText(), /90/);
  assert.match(
    await page.locator('.ao-metrics-primary').innerText(),
    /已记录小计/,
  );
  checks.push(
    'currencies stay separate and incomplete charges are labelled subtotal',
  );
  await page.getByRole('button', { name: 'CPU', exact: true }).click();
  assert.equal(await page.locator('.ao-trend-svg .ao-chart-line').count(), 2);
  assert.equal(await page.locator('.ao-gauge-needle').count(), 3);
  await page
    .getByRole('button', { name: 'Token / 对话次数', exact: true })
    .click();
  const point = page.locator('.ao-chart-point').first();
  await point.focus();
  await page.keyboard.press('End');
  assert.match(await page.locator('.ao-trend-readout').innerText(), /18:00/);
  assert.match(
    await page.locator('.ao-trend-readout').innerText(),
    /已记录对话/,
  );
  checks.push('resource and conversation chart tab/keyboard/readout');
  const nodeButton = page.getByRole('button', {
    name: '节点明细',
    exact: true,
  });
  assert.equal(
    await nodeButton.evaluate((el) => !!el.closest('.ao-resource-panel')),
    true,
  );
  await nodeButton.click();
  await page.getByRole('button', { name: '下一页', exact: true }).click();
  assert.equal(await page.locator('.ao-nodes-dialog tbody tr').count(), 2);
  await page.getByRole('button', { name: '搜索节点', exact: true }).click();
  await page
    .getByRole('textbox', { name: '搜索节点', exact: true })
    .fill('worker-11');
  assert.equal(await page.locator('.ao-nodes-dialog tbody tr').count(), 1);
  await page
    .getByRole('button', { name: 'worker-11 · 详情', exact: true })
    .click();
  await page.getByRole('dialog', { name: 'worker-11', exact: true }).waitFor();
  nodeCpu = 2;
  await page.evaluate(() => {
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      get: () => 'hidden',
    });
    document.dispatchEvent(new Event('visibilitychange'));
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      get: () => 'visible',
    });
    document.dispatchEvent(new Event('visibilitychange'));
  });
  await page.waitForFunction(() =>
    document
      .querySelector('.ao-node-detail .ao-gauge')
      ?.getAttribute('aria-label')
      ?.includes('25%'),
  );
  await page.keyboard.press('Escape');
  await page.keyboard.press('Escape');
  assert.ok(await nodeButton.evaluate((el) => document.activeElement === el));
  checks.push(
    'node search/pagination/nested live detail/focus refetch/Escape/focus return',
  );
  for (const lang of ['zh', 'en', 'ja', 'ko', 'de']) {
    locale = lang;
    await page.evaluate(
      (lang) => localStorage.setItem('clawmanager_locale', lang),
      lang,
    );
    for (const [width, height] of [
      [2560, 1271],
      [1920, 720],
      [1440, 900],
      [1280, 720],
      [768, 900],
      [390, 900],
    ]) {
      await page.setViewportSize({ width, height });
      mode = 'normal';
      await load();
      assert.equal(await page.locator('html').getAttribute('lang'), lang);
      const geometry = await page.evaluate(() => {
        const main = document.querySelector('.cm-shell-main'),
          r = (selector) => {
            const b = document.querySelector(selector).getBoundingClientRect();
            return { x: b.x, w: b.width, y: b.y, b: b.bottom };
          };
        return {
          width: innerWidth,
          scrollWidth: document.documentElement.scrollWidth,
          mainHeight: main.clientHeight,
          mainScroll: main.scrollHeight,
          left: r('.ao-metrics-primary'),
          right: r('.ao-metrics-health'),
          trend: r('.ao-trend-panel'),
          resources: r('.ao-resource-panel'),
          types: r('.ao-types-panel'),
          models: r('.ao-models-panel'),
        };
      });
      assert.ok(
        geometry.scrollWidth <= width + 1,
        JSON.stringify({ lang, ...geometry }),
      );
      if (width >= 1100) {
        assert.ok(
          geometry.mainScroll <= geometry.mainHeight + 1,
          JSON.stringify({ lang, ...geometry }),
        );
        for (const [a, b] of [
          ['left', 'trend'],
          ['left', 'types'],
          ['right', 'resources'],
          ['right', 'models'],
        ])
          for (const key of ['x', 'w'])
            assert.ok(
              Math.abs(geometry[a][key] - geometry[b][key]) < 1,
              JSON.stringify({ lang, width, a, b, key, geometry }),
            );
      }
      if (width === 2560) {
        for (const selector of [
          '.ao-resource-panel .ao-gauge',
          '.ao-type-pies .ao-pie',
          '.ao-model-content .ao-pie',
        ]) {
          const chart = await page.locator(selector).first().boundingBox();
          assert.ok(
            chart.width >= 240,
            JSON.stringify({ lang, selector, chart }),
          );
        }
      }
      if (width < 1100) {
        assert.ok(
          geometry.types.y >= geometry.resources.b - 1,
          JSON.stringify({
            lang,
            width,
            problem: 'resource and distribution cards overlap',
            geometry,
          }),
        );
        const mobileDetails = page.getByRole('button', {
          name:
            lang === 'zh'
              ? '节点明细'
              : lang === 'en'
                ? 'Node details'
                : lang === 'ja'
                  ? 'ノード詳細'
                  : lang === 'ko'
                    ? '노드 상세'
                    : 'Knotendetails',
          exact: true,
        });
        await mobileDetails.click();
        await page.locator('.ao-nodes-dialog[open]').waitFor();
        await page.keyboard.press('Escape');
      }
      if (lang === 'zh' && width >= 1100)
        await page.screenshot({
          path: out + `/overview-${width}.jpg`,
          type: 'jpeg',
          quality: 85,
        });
    }
  }
  checks.push(
    'five languages × desktop/tablet/mobile, aligned groups and desktop one-screen',
  );
  locale = 'zh';
  await page.evaluate(() => localStorage.setItem('clawmanager_locale', 'zh'));
  await page.setViewportSize({ width: 1440, height: 900 });
  for (const state of ['empty', 'missing', 'error', 'loading']) {
    mode = state;
    await load();
    if (state === 'missing') {
      assert.equal(await page.locator('.ao-gauge-needle').count(), 0);
      assert.equal(await page.locator('.ao-chart-line').count(), 0);
      await page.getByRole('button', { name: 'CPU', exact: true }).click();
      assert.equal(await page.locator('.ao-chart-line').count(), 1);
    }
    if (state === 'error') {
      await page.getByRole('button', { name: '重试', exact: true }).waitFor();
      mode = 'normal';
      await page.getByRole('button', { name: '重试', exact: true }).click();
      await page
        .getByRole('combobox', { name: '费用币种', exact: true })
        .waitFor();
    }
    await page.screenshot({
      path: out + `/${state}.jpg`,
      type: 'jpeg',
      quality: 80,
    });
  }
  checks.push('known zero, missing actuals/usage, retry recovery, loading');
  assert.deepEqual(errors, []);
  await fs.writeFile(
    out + '/verification.json',
    JSON.stringify(
      {
        status: 'PASS',
        checks,
        reads,
        errors,
        locales: ['zh', 'en', 'ja', 'ko', 'de'],
      },
      null,
      2,
    ),
  );
  console.log(JSON.stringify({ status: 'PASS', checks, reads }));
} finally {
  await browser.close();
}
