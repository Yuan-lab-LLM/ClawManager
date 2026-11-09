(function (scope) {
  "use strict";
  if (window.__clawmanagerOpenCodeStorageScope === scope) return;
  var prefix = "clawmanager-opencode:" + scope + ":";

  function isolateStorage(name) {
    var storage = window[name];
    function keys() {
      var result = [];
      for (var i = 0; i < storage.length; i++) {
        var key = storage.key(i);
        if (key !== null && key.indexOf(prefix) === 0) result.push(key.slice(prefix.length));
      }
      return result;
    }
    var methods = {
      getItem: function (key) { return storage.getItem(prefix + String(key)); },
      setItem: function (key, value) { storage.setItem(prefix + String(key), value); },
      removeItem: function (key) { storage.removeItem(prefix + String(key)); },
      clear: function () { keys().forEach(function (key) { storage.removeItem(prefix + key); }); },
      key: function (index) { var key = keys()[Number(index) >>> 0]; return key === undefined ? null : key; }
    };
    var scoped = new Proxy(storage, {
      get: function (target, property) {
        if (property === "length") return keys().length;
        if (Object.prototype.hasOwnProperty.call(methods, property)) return methods[property];
        if (typeof property !== "string" || property in Storage.prototype) {
          var value = Reflect.get(target, property, target);
          return typeof value === "function" ? value.bind(target) : value;
        }
        var item = methods.getItem(property);
        return item === null ? undefined : item;
      },
      set: function (target, property, value) {
        if (typeof property !== "string") return Reflect.set(target, property, value, target);
        methods.setItem(property, value);
        return true;
      },
      deleteProperty: function (target, property) {
        if (typeof property !== "string") return Reflect.deleteProperty(target, property);
        methods.removeItem(property);
        return true;
      },
      has: function (target, property) {
        return property in Storage.prototype || methods.getItem(property) !== null;
      },
      ownKeys: function () { return keys(); },
      getOwnPropertyDescriptor: function (target, property) {
        var value = methods.getItem(property);
        if (value !== null) return {configurable: true, enumerable: true, writable: true, value: value};
      },
      defineProperty: function (target, property, descriptor) {
        if (typeof property !== "string" || !("value" in descriptor)) return false;
        methods.setItem(property, descriptor.value);
        return true;
      }
    });
    Object.defineProperty(window, name, {configurable: true, get: function () { return scoped; }});
  }

  // Other namespaces and legacy entries remain intact. Only this instance's
  // logical storage is exposed to OpenCode, including property enumeration.
  ["localStorage", "sessionStorage"].forEach(function (name) {
    try { isolateStorage(name); } catch (_) { /* Preserve browser privacy-mode behavior. */ }
  });
  try {
    var factory = window.indexedDB;
    if (factory) {
      var scopedFactory = new Proxy(factory, {
        get: function (target, property) {
          if (property === "open") return function (name, version) {
            return version === undefined ? factory.open(prefix + String(name)) : factory.open(prefix + String(name), version);
          };
          if (property === "deleteDatabase") return function (name) { return factory.deleteDatabase(prefix + String(name)); };
          if (property === "databases" && typeof factory.databases === "function") return function () {
            return factory.databases().then(function (items) {
              return items.filter(function (item) { return item.name && item.name.indexOf(prefix) === 0; })
                .map(function (item) { return Object.assign({}, item, {name: item.name.slice(prefix.length)}); });
            });
          };
          var value = Reflect.get(target, property, target);
          return typeof value === "function" ? value.bind(target) : value;
        }
      });
      Object.defineProperty(window, "indexedDB", {configurable: true, get: function () { return scopedFactory; }});
    }
  } catch (_) { /* Keep the upstream fallback when IndexedDB is unavailable. */ }
  window.__clawmanagerOpenCodeStorageScope = scope;
})(__CLAWMANAGER_OPENCODE_SCOPE__);
