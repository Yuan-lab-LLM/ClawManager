import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'

// Execute the production Go raw-string constant, not a second implementation
// of URL rewriting. No runtime, cluster, cookie or network access is involved.
const source = readFileSync(new URL('../backend/internal/services/hermes_dashboard_assets.go', import.meta.url), 'utf8')
const match = source.match(/const hermesDashboardAssetBridge = `([\s\S]*?)`/)
assert(match, 'Production Hermes asset bridge constant was not found')
const template = match[1]
assert.equal(template.split('__CM_HERMES_ASSET_BASE__').length, 2, 'Expected one Go-injected instance prefix')

function createRealm(instanceId, install = true) {
  const prefix = `/api/v1/instances/${instanceId}/proxy`
  const context = vm.createContext({ URL, fixturePrefix: prefix })
  vm.runInContext(`
    globalThis.window = globalThis;
    globalThis.location = { origin: 'https://manager.example:39443' };
    globalThis.nativeCalls = [];
    globalThis.requests = [];
    globalThis.document = {
      baseURI: location.origin + fixturePrefix + '/',
      head: { appendChild(node) {
        requests.push(node instanceof HTMLLinkElement ? node.href : node.src);
        return node;
      } },
    };
    globalThis.Element = class Element {
      constructor() { this.attributes = Object.create(null); }
      getAttribute(name) { return this.attributes[String(name).toLowerCase()] ?? null; }
      setAttribute(name, value) {
        if (!(this instanceof Element)) throw new TypeError('Illegal invocation');
        name = String(name).toLowerCase();
        nativeCalls.push({ receiver: this, method: 'setAttribute', name, value });
        this.attributes[name] = String(value);
      }
    };
    globalThis.HTMLLinkElement = class HTMLLinkElement extends Element {};
    globalThis.HTMLScriptElement = class HTMLScriptElement extends Element {};
    globalThis.nativeDescriptors = {};
    for (const [Constructor, name] of [[HTMLLinkElement, 'href'], [HTMLScriptElement, 'src']]) {
      Object.defineProperty(Constructor.prototype, name, {
        configurable: true,
        enumerable: true,
        get() {
          if (!(this instanceof Constructor)) throw new TypeError('Illegal invocation');
          return new URL(this.getAttribute(name) || '', document.baseURI).href;
        },
        set(value) {
          if (!(this instanceof Constructor)) throw new TypeError('Illegal invocation');
          nativeCalls.push({ receiver: this, method: 'setter', name, value });
          this.attributes[name] = String(value);
        },
      });
      nativeDescriptors[name] = Object.getOwnPropertyDescriptor(Constructor.prototype, name);
    }
    globalThis.nativeSetAttribute = Element.prototype.setAttribute;
  `, context, { filename: 'hermes-assets-synthetic-dom.js' })
  if (install) installBridge(context, prefix)
  return { context, prefix, origin: context.location.origin }
}

function installBridge(context, prefix) {
  const html = template.replace('__CM_HERMES_ASSET_BASE__', JSON.stringify(prefix))
  const start = html.indexOf('>')
  const end = html.lastIndexOf('</script>')
  assert(start >= 0 && end > start, 'Production bridge must be one script element')
  vm.runInContext(html.slice(start + 1, end), context, { filename: 'hermes_dashboard_assets.go:hermesDashboardAssetBridge' })
}

function element(realm, kind = 'link') {
  return vm.runInContext(kind === 'link' ? 'new HTMLLinkElement()' : 'new HTMLScriptElement()', realm.context)
}

test('Native modulepreload and dynamic script URL setters are scoped before insertion', () => {
  for (const id of [214, 215]) {
    const realm = createRealm(id)
    for (const kind of ['link', 'script']) {
      const attribute = kind === 'link' ? 'href' : 'src'
      const path = '/assets/vendor-B7eZBWFZ.js'
      // The deployed Vite helper calls import.meta.resolve(), producing an
      // absolute URL before assigning link.href. Cover that actual shape.
      for (const value of [path, realm.origin + path, new URL(realm.origin + path)]) {
        const node = element(realm, kind)
        node[attribute] = value
        assert.equal(node.getAttribute(attribute), realm.prefix + path)
        assert.equal(node[attribute], realm.origin + realm.prefix + path)
        assert.equal(realm.context.document.head.appendChild(node), node)
        assert.equal(realm.context.requests.at(-1), realm.origin + realm.prefix + path)
      }
    }
  }
})

test('setAttribute handles same-origin absolute URLs and URL objects for both resource elements', () => {
  const realm = createRealm(214)
  for (const [kind, attribute] of [['link', 'href'], ['script', 'src']]) {
    for (const value of ['/assets/ChatPage-oNWcZI--.js', realm.origin + '/assets/ChatPage-oNWcZI--.js', new URL(realm.origin + '/assets/ChatPage-oNWcZI--.js')]) {
      const node = element(realm, kind)
      assert.equal(node.setAttribute(attribute.toUpperCase(), value), undefined)
      assert.equal(node.getAttribute(attribute), realm.prefix + '/assets/ChatPage-oNWcZI--.js')
      const call = realm.context.nativeCalls.at(-1)
      assert.equal(call.method, 'setAttribute')
      assert.equal(call.receiver, node)
      assert.equal(call.name, attribute)
    }
  }
})

test('Stylesheets, both font directories, images and favicon remain instance scoped', () => {
  const realm = createRealm(214)
  for (const path of ['/assets/index-mXYHKWVT.css', '/fonts/regular.woff2', '/fonts-terminal/JetBrainsMono-Regular.woff2', '/ds-assets/icon.svg', '/favicon.ico']) {
    for (const assign of ['setter', 'attribute']) {
      const node = element(realm)
      if (assign === 'setter') node.href = new URL(realm.origin + path)
      else node.setAttribute('href', realm.origin + path)
      assert.equal(node.getAttribute('href'), realm.prefix + path)
      realm.context.document.head.appendChild(node)
      assert.equal(realm.context.requests.at(-1), realm.origin + realm.prefix + path)
    }
  }
})

test('Existing prefixes, other instances, external origins, APIs and navigation are never rewritten', () => {
  const realm = createRealm(214)
  const values = [
    realm.prefix + '/assets/already.js',
    realm.origin + realm.prefix + '/assets/already.js',
    '/api/v1/instances/215/proxy/assets/other.js',
    realm.origin + '/api/v1/instances/215/proxy/assets/other.js',
    'https://cdn.example/assets/external.js',
    '//cdn.example/assets/external.js',
    'http://manager.example:39443/assets/different-scheme.js',
    '/api/status',
    '/api/v1/instances/214/hermes-desktop/session',
    '/chat',
    '/sessions',
    '/assets/not-an-allowed-resource.yaml',
  ]
  for (const [kind, attribute] of [['link', 'href'], ['script', 'src']]) {
    for (const value of values) {
      const node = element(realm, kind)
      node[attribute] = value
      assert.equal(node.getAttribute(attribute), value)
      node.setAttribute(attribute, value)
      assert.equal(node.getAttribute(attribute), value)
    }
  }
  const unrelated = vm.runInContext('new Element()', realm.context)
  unrelated.setAttribute('href', '/assets/user-content.js')
  assert.equal(unrelated.getAttribute('href'), '/assets/user-content.js')
  const link = element(realm)
  link.setAttribute('data-url', '/assets/user-content.js')
  assert.equal(link.getAttribute('data-url'), '/assets/user-content.js')
  const script = element(realm, 'script')
  script.setAttribute('href', '/assets/not-a-script-src.js')
  assert.equal(script.getAttribute('href'), '/assets/not-a-script-src.js')
})

test('Separate iframe realms retain independent prototypes and instance prefixes', () => {
  const first = createRealm(214)
  const second = createRealm(215, false)
  assert.notEqual(first.context.HTMLLinkElement.prototype, second.context.HTMLLinkElement.prototype)
  const unpatched = element(second)
  unpatched.href = '/assets/vendor-B7eZBWFZ.js'
  assert.equal(unpatched.getAttribute('href'), '/assets/vendor-B7eZBWFZ.js', 'First iframe must not patch another realm')
  installBridge(second.context, second.prefix)
  for (const realm of [first, second, first, second]) {
    const node = element(realm)
    node.href = '/assets/vendor-B7eZBWFZ.js'
    assert.equal(node.getAttribute('href'), realm.prefix + '/assets/vendor-B7eZBWFZ.js')
  }
  const firstLink = element(first)
  firstLink.href = second.prefix + '/assets/vendor-B7eZBWFZ.js'
  assert.equal(firstLink.getAttribute('href'), second.prefix + '/assets/vendor-B7eZBWFZ.js', 'Must never rebind another instance URL')
})

test('Wrapping preserves native getters, descriptor flags, receivers and setter behavior', () => {
  const realm = createRealm(214)
  for (const [kind, attribute] of [['link', 'href'], ['script', 'src']]) {
    const Constructor = kind === 'link' ? realm.context.HTMLLinkElement : realm.context.HTMLScriptElement
    const actual = Object.getOwnPropertyDescriptor(Constructor.prototype, attribute)
    const original = realm.context.nativeDescriptors[attribute]
    assert.equal(actual.get, original.get)
    assert.equal(actual.enumerable, original.enumerable)
    assert.equal(actual.configurable, original.configurable)
    assert.notEqual(actual.set, original.set)
    const node = element(realm, kind)
    const before = realm.context.nativeCalls.length
    node[attribute] = '/assets/native.js'
    assert.equal(realm.context.nativeCalls.length, before + 1, 'Native setter must run exactly once')
    const call = realm.context.nativeCalls.at(-1)
    assert.equal(call.receiver, node)
    assert.equal(call.method, 'setter')
    assert.equal(call.value, realm.prefix + '/assets/native.js')
    assert.throws(() => actual.set.call({}, '/assets/native.js'), /Illegal invocation/)
  }
})
