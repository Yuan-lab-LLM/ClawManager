(function () {
  "use strict";
  if (window.parent === window) return;
  var parentOrigin = "", messageType = "";
  window.addEventListener("message", function (event) {
    if (event.source === window.parent && event.data && typeof event.data.type === "string" && /^clawmanager:preview-bridge:activate:[0-9a-f-]{36}$/.test(event.data.type)) {
      parentOrigin = event.origin;
      messageType = event.data.type.replace(":activate:", ":open:");
    }
  });
  document.addEventListener("click", function (event) {
    if (!parentOrigin || !event.isTrusted || event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    var node = event.target instanceof Element ? event.target.closest("a[href]") : null;
    if (!node || node.hasAttribute("download")) return;
    var value = node.getAttribute("href");
    if (!value || value.length > 8192 || /[\x00-\x20\x7f]/.test(value)) return;
    var url;
    try { url = new URL(value); } catch (_) {
      // Markdown autolinks can swallow adjacent Chinese prose after the port.
      // Recover only an exact loopback authority; valid paths stay untouched.
      var authority = value.match(/^(https?:\/\/(?:localhost|127\.0\.0\.1|\[::1\]|0\.0\.0\.0)(?::[0-9]+)?)[\uff08\uff09\uff0c\u3002\uff1b\uff01\uff1f\u3001\u3010\u3011\u300a\u300b\u300c\u300d\u300e\u300f]/i);
      if (!authority) return;
      try { url = new URL(authority[1]); } catch (_) { return; }
    }
    if (["http:", "https:"].indexOf(url.protocol) < 0 || url.username || url.password || ["localhost", "127.0.0.1", "[::1]", "0.0.0.0"].indexOf(url.hostname) < 0) return;
    if (url.port && Number(url.port) < 1) return;
    if (url.hostname === "0.0.0.0") url.hostname = "localhost";
    event.preventDefault();
    event.stopImmediatePropagation();
    window.parent.postMessage({ type: messageType, url: url.href }, parentOrigin);
  }, true);
})();
