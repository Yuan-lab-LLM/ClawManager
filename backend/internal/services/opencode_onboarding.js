(function () {
  "use strict";

  var rootID = "clawmanager-opencode-onboarding-root";
  var storageKey = "clawmanager-opencode-onboarding-v1-dismissed";
  var createProjectPath = "/__clawmanager/onboarding/project-directory";
  var canCreateProject = __CLAWMANAGER_OPENCODE_CAN_CREATE__;

  if (window.__clawmanagerOpenCodeOnboardingMounted) return;
  window.__clawmanagerOpenCodeOnboardingMounted = true;

  function dismissed() {
    try {
      return window.localStorage.getItem(storageKey) === "true";
    } catch (_) {
      return false;
    }
  }

  function rememberDismissal() {
    try {
      window.localStorage.setItem(storageKey, "true");
    } catch (_) {
      // Storage can be unavailable in browser privacy modes. The current page
      // can still hide the panel; the next navigation will show it again.
    }
  }

  function mount() {
    if (!document.body || document.getElementById(rootID)) return;

    var host = document.createElement("div");
    host.id = rootID;
    host.setAttribute("data-onboarding-version", "1");
    var root = host.attachShadow ? host.attachShadow({ mode: "open" }) : host;

    root.innerHTML = `
      <style>
        :host {
          all: initial;
          position: fixed;
          inset: 0;
          z-index: 2147483000;
          pointer-events: none;
          color-scheme: light;
        }
        * { box-sizing: border-box; }
        button { font: inherit; }
        .cm-oc-guide {
          position: fixed;
          top: 20px;
          right: 20px;
          width: min(390px, calc(100vw - 40px));
          max-height: calc(100vh - 40px);
          overflow: auto;
          border: 1px solid #dbe4f0;
          border-radius: 14px;
          background: #ffffff;
          box-shadow: 0 18px 50px rgba(15, 23, 42, 0.2);
          color: #0f172a;
          font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Microsoft YaHei", sans-serif;
          pointer-events: auto;
        }
        .cm-oc-guide[hidden], .cm-oc-help[hidden] { display: none; }
        .cm-oc-header {
          display: flex;
          align-items: flex-start;
          justify-content: space-between;
          gap: 16px;
          padding: 18px 18px 12px;
        }
        .cm-oc-eyebrow {
          margin: 0 0 4px;
          color: #4f46e5;
          font-size: 12px;
          font-weight: 700;
          letter-spacing: 0.04em;
        }
        .cm-oc-title {
          margin: 0;
          font-size: 18px;
          font-weight: 700;
          line-height: 1.35;
        }
        .cm-oc-close {
          display: inline-grid;
          width: 32px;
          height: 32px;
          flex: 0 0 auto;
          place-items: center;
          border: 1px solid transparent;
          border-radius: 8px;
          background: transparent;
          color: #64748b;
          cursor: pointer;
          font-size: 22px;
          line-height: 1;
        }
        .cm-oc-close:hover { background: #f1f5f9; color: #0f172a; }
        .cm-oc-body { padding: 0 18px 16px; }
        .cm-oc-intro {
          margin: 0 0 14px;
          color: #475569;
          font-size: 13px;
          line-height: 1.65;
        }
        .cm-oc-steps {
          display: grid;
          gap: 10px;
          margin: 0;
          padding: 0;
          list-style: none;
        }
        .cm-oc-step {
          display: grid;
          grid-template-columns: 28px minmax(0, 1fr);
          gap: 10px;
          padding: 11px;
          border: 1px solid #e2e8f0;
          border-radius: 10px;
          background: #f8fafc;
        }
        .cm-oc-number {
          display: grid;
          width: 28px;
          height: 28px;
          place-items: center;
          border-radius: 999px;
          background: #e0e7ff;
          color: #4338ca;
          font-size: 13px;
          font-weight: 700;
        }
        .cm-oc-step-title {
          margin: 1px 0 3px;
          font-size: 14px;
          font-weight: 700;
          line-height: 1.45;
        }
        .cm-oc-step-copy {
          margin: 0;
          color: #475569;
          font-size: 12px;
          line-height: 1.6;
        }
        .cm-oc-locate {
          margin-top: 8px;
          padding: 6px 10px;
          border: 1px solid #a5b4fc;
          border-radius: 7px;
          background: #eef2ff;
          color: #3730a3;
          cursor: pointer;
          font-size: 12px;
          font-weight: 700;
        }
        .cm-oc-locate-status { margin: 7px 0 0; color: #475569; font-size: 12px; line-height: 1.5; }
        .cm-oc-spotlight {
          position: fixed;
          z-index: 2147483001;
          border: 3px solid #6366f1;
          border-radius: 9px;
          box-shadow: 0 0 0 5px rgba(99, 102, 241, 0.22);
          pointer-events: none;
        }
        .cm-oc-spotlight[hidden] { display: none; }
        .cm-oc-pointer {
          position: fixed;
          z-index: 2147483002;
          max-width: min(240px, calc(100vw - 24px));
          padding: 8px 11px;
          border-radius: 8px;
          background: #4338ca;
          box-shadow: 0 5px 18px rgba(15, 23, 42, 0.2);
          color: white;
          font: 600 12px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", "Microsoft YaHei", sans-serif;
          pointer-events: none;
        }
        .cm-oc-pointer[hidden] { display: none; }
        .cm-oc-create {
          display: grid;
          grid-template-columns: minmax(0, 1fr) auto;
          gap: 8px;
          margin-top: 9px;
        }
        .cm-oc-create[hidden] { display: none; }
        .cm-oc-input {
          min-width: 0;
          height: 34px;
          padding: 6px 9px;
          border: 1px solid #cbd5e1;
          border-radius: 7px;
          background: #ffffff;
          color: #0f172a;
          font: inherit;
          font-size: 12px;
        }
        .cm-oc-input::placeholder { color: #94a3b8; }
        .cm-oc-input:focus { border-color: #6366f1; outline: 3px solid rgba(99, 102, 241, 0.16); }
        .cm-oc-create-button {
          min-height: 34px;
          padding: 6px 10px;
          border: 1px solid #a5b4fc;
          border-radius: 7px;
          background: #eef2ff;
          color: #4338ca;
          cursor: pointer;
          font-size: 12px;
          font-weight: 700;
        }
        .cm-oc-create-button:hover { background: #e0e7ff; }
        .cm-oc-create-button:disabled, .cm-oc-input:disabled { cursor: not-allowed; opacity: 0.65; }
        .cm-oc-create-status {
          grid-column: 1 / -1;
          min-height: 0;
          margin: 0;
          color: #475569;
          font-size: 12px;
          line-height: 1.5;
        }
        .cm-oc-create-status:empty { display: none; }
        .cm-oc-create-status[data-tone="success"] { color: #047857; }
        .cm-oc-create-status[data-tone="error"] { color: #b91c1c; }
        .cm-oc-note {
          margin: 12px 0 0;
          padding: 9px 11px;
          border-radius: 8px;
          background: #eef2ff;
          color: #3730a3;
          font-size: 12px;
          line-height: 1.55;
        }
        .cm-oc-footer {
          display: flex;
          align-items: center;
          justify-content: flex-end;
          padding: 0 18px 18px;
        }
        .cm-oc-primary {
          min-height: 36px;
          padding: 8px 15px;
          border: 0;
          border-radius: 8px;
          background: #4f46e5;
          color: #ffffff;
          cursor: pointer;
          font-size: 13px;
          font-weight: 650;
        }
        .cm-oc-primary:hover { background: #4338ca; }
        .cm-oc-help {
          position: fixed;
          right: 20px;
          bottom: 20px;
          display: inline-flex;
          min-height: 38px;
          align-items: center;
          gap: 7px;
          padding: 8px 13px;
          border: 1px solid #cbd5e1;
          border-radius: 999px;
          background: #ffffff;
          box-shadow: 0 8px 25px rgba(15, 23, 42, 0.16);
          color: #334155;
          cursor: pointer;
          font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Microsoft YaHei", sans-serif;
          font-size: 13px;
          font-weight: 650;
          pointer-events: auto;
        }
        .cm-oc-help:hover { border-color: #818cf8; color: #4338ca; }
        .cm-oc-help-mark {
          display: grid;
          width: 20px;
          height: 20px;
          place-items: center;
          border-radius: 999px;
          background: #e0e7ff;
          color: #4338ca;
          font-weight: 800;
        }
        button:focus-visible {
          outline: 3px solid rgba(99, 102, 241, 0.35);
          outline-offset: 2px;
        }
        @media (max-width: 640px) {
          .cm-oc-guide {
            top: auto;
            right: 12px;
            bottom: 12px;
            left: 12px;
            width: auto;
            max-height: calc(100vh - 24px);
          }
          .cm-oc-help { right: 12px; bottom: 12px; }
          .cm-oc-create { grid-template-columns: 1fr; }
          .cm-oc-create-status { grid-column: 1; }
        }
        @media (prefers-reduced-motion: no-preference) {
          .cm-oc-guide, .cm-oc-help { animation: cm-oc-enter 160ms ease-out; }
          @keyframes cm-oc-enter {
            from { opacity: 0; transform: translateY(-6px); }
            to { opacity: 1; transform: translateY(0); }
          }
        }
      </style>
      <section class="cm-oc-guide" role="dialog" aria-modal="false" aria-labelledby="cm-oc-title">
        <header class="cm-oc-header">
          <div>
            <p class="cm-oc-eyebrow">\u9996\u6b21\u4f7f\u7528</p>
            <h2 class="cm-oc-title" id="cm-oc-title">OpenCode \u5feb\u901f\u5f00\u59cb</h2>
          </div>
          <button class="cm-oc-close" type="button" aria-label="\u5173\u95ed\u65b0\u624b\u5f15\u5bfc" title="\u5173\u95ed">\u00d7</button>
        </header>
        <div class="cm-oc-body">
          <p class="cm-oc-intro">\u6309\u7167\u4e0b\u9762\u4e09\u6b65\u51c6\u5907\u9879\u76ee\u5e76\u5f00\u59cb\u7b2c\u4e00\u6b21\u5bf9\u8bdd\u3002</p>
          <ol class="cm-oc-steps">
            <li class="cm-oc-step">
              <span class="cm-oc-number">1</span>
              <div>
                <h3 class="cm-oc-step-title">\u521b\u5efa\u6216\u9009\u62e9\u6587\u4ef6\u5939</h3>
                <p class="cm-oc-step-copy cm-oc-create-copy">\u8f93\u5165\u540d\u79f0\u521b\u5efa\u65b0\u7684\u9879\u76ee\u6587\u4ef6\u5939\uff0c\u6216\u8005\u76f4\u63a5\u4f7f\u7528\u5df2\u7ecf\u5b58\u5728\u7684\u9879\u76ee\u76ee\u5f55\u3002</p>
                <form class="cm-oc-create">
                  <input class="cm-oc-input" type="text" maxlength="128" autocomplete="off" spellcheck="false" aria-label="\u9879\u76ee\u6587\u4ef6\u5939\u540d\u79f0" placeholder="\u4f8b\u5982 my-project">
                  <button class="cm-oc-create-button" type="submit">\u521b\u5efa\u6587\u4ef6\u5939</button>
                  <p class="cm-oc-create-status" role="status" aria-live="polite"></p>
                </form>
              </div>
            </li>
            <li class="cm-oc-step">
              <span class="cm-oc-number">2</span>
              <div>
                <h3 class="cm-oc-step-title">\u6dfb\u52a0\u5e76\u9009\u4e2d\u9879\u76ee</h3>
                <p class="cm-oc-step-copy">\u5728 OpenCode \u5de6\u4fa7\u201c\u9879\u76ee\u201d\u533a\u6dfb\u52a0\u5e76\u9009\u4e2d\u6587\u4ef6\u5939\u3002\u53f3\u4fa7\u6587\u4ef6\u9762\u677f\u770b\u5230\u6587\u4ef6\u5939\uff0c\u4e0d\u4ee3\u8868\u5df2\u9009\u4e3a OpenCode \u9879\u76ee\u3002</p>
                <button class="cm-oc-locate" type="button" data-target="project">\u5e26\u6211\u627e\u5230\u6dfb\u52a0\u9879\u76ee</button>
                <p class="cm-oc-locate-status" data-status="project" role="status" aria-live="polite"></p>
              </div>
            </li>
            <li class="cm-oc-step">
              <span class="cm-oc-number">3</span>
              <div>
                <h3 class="cm-oc-step-title">\u521b\u5efa\u65b0\u4f1a\u8bdd</h3>
                <p class="cm-oc-step-copy">\u786e\u8ba4\u5de6\u4fa7\u9879\u76ee\u5217\u8868\u4e2d\u5df2\u9009\u4e2d\u9879\u76ee\uff0c\u518d\u521b\u5efa\u4f1a\u8bdd\u3002</p>
                <button class="cm-oc-locate" type="button" data-target="session">\u5e26\u6211\u627e\u5230\u65b0\u5efa\u4f1a\u8bdd</button>
                <p class="cm-oc-locate-status" data-status="session" role="status" aria-live="polite"></p>
              </div>
            </li>
          </ol>
          <p class="cm-oc-note">\u7cfb\u7edf\u4e0d\u4f1a\u81ea\u52a8\u521b\u5efa\u6587\u4ef6\u5939\u6216\u4f1a\u8bdd\uff0c\u6240\u6709\u64cd\u4f5c\u90fd\u7531\u4f60\u786e\u8ba4\u5b8c\u6210\u3002</p>
        </div>
        <footer class="cm-oc-footer">
          <button class="cm-oc-primary" type="button">\u6211\u77e5\u9053\u4e86</button>
        </footer>
      </section>
      <div class="cm-oc-spotlight" hidden aria-hidden="true"></div>
      <div class="cm-oc-pointer" hidden aria-hidden="true"></div>
      <button class="cm-oc-help" type="button" aria-label="\u6253\u5f00 OpenCode \u4f7f\u7528\u5e2e\u52a9">
        <span class="cm-oc-help-mark" aria-hidden="true">?</span>
        <span>\u4f7f\u7528\u5e2e\u52a9</span>
      </button>
    `;

    var panel = root.querySelector(".cm-oc-guide");
    var help = root.querySelector(".cm-oc-help");
    var close = root.querySelector(".cm-oc-close");
    var acknowledge = root.querySelector(".cm-oc-primary");
    var createCopy = root.querySelector(".cm-oc-create-copy");
    var createForm = root.querySelector(".cm-oc-create");
    var createInput = root.querySelector(".cm-oc-input");
    var createButton = root.querySelector(".cm-oc-create-button");
    var createStatus = root.querySelector(".cm-oc-create-status");
    var spotlight = root.querySelector(".cm-oc-spotlight");
    var pointer = root.querySelector(".cm-oc-pointer");
    var activeTarget = null;
    var activeKind = "";
    var updateQueued = false;

    function buttonName(element) {
      return [element.getAttribute("aria-label"), element.getAttribute("title"), element.textContent]
        .filter(Boolean).join(" ").trim();
    }

    function visible(element) {
      if (!element || host.contains(element)) return false;
      var rect = element.getBoundingClientRect();
      var style = window.getComputedStyle(element);
      return rect.width > 0 && rect.height > 0 && style.visibility !== "hidden" && style.display !== "none";
    }

    function findTarget(kind) {
      var controls = Array.from(document.querySelectorAll('button, [role="button"]')).filter(visible);
      var pattern = kind === "project" ? /\u6dfb\u52a0\u9879\u76ee|add project|open project/i : /\u65b0\u5efa\u4f1a\u8bdd|\u521b\u5efa\u4f1a\u8bdd|new session|new chat/i;
      var exact = controls.find(function (control) { return pattern.test(buttonName(control)); });
      if (exact) return exact;
      if (kind === "project") {
        // Some OpenCode layouts expose only an icon beside the Projects heading.
        var headings = Array.from(document.querySelectorAll("h1, h2, h3, h4, span, div"))
          .filter(function (element) { return visible(element) && /^(\u9879\u76ee|projects)$/i.test(element.textContent.trim()); });
        for (var i = 0; i < headings.length; i++) {
          var heading = headings[i];
          var nearby = controls.find(function (control) {
            var a = heading.getBoundingClientRect();
            var b = control.getBoundingClientRect();
            return b.left >= a.right - 8 && b.left - a.right < 100 && Math.abs(b.top - a.top) < 24;
          });
          if (nearby) return nearby;
        }
      }
      return null;
    }

    function clearSpotlight() {
      if (activeKind) {
        var status = root.querySelector('[data-status="' + activeKind + '"]');
        if (status) status.textContent = "";
      }
      activeKind = "";
      activeTarget = null;
      spotlight.hidden = true;
      pointer.hidden = true;
    }

    function updateSpotlight() {
      updateQueued = false;
      if (!activeKind || panel.hidden) return;
      activeTarget = findTarget(activeKind);
      var status = root.querySelector('[data-status="' + activeKind + '"]');
      if (!activeTarget) {
        spotlight.hidden = true;
        pointer.hidden = true;
        status.textContent = activeKind === "project"
          ? "\u6682\u672a\u627e\u5230\u6309\u94ae\u3002\u8bf7\u5728 OpenCode \u5de6\u4fa7\u627e\u201c\u9879\u76ee\u201d\u6807\u9898\u65c1\u7684\u6587\u4ef6\u5939+\u56fe\u6807\uff1b\u5982\u679c\u4fa7\u680f\u6298\u53e0\uff0c\u8bf7\u5148\u5c55\u5f00\u3002"
          : "\u6682\u672a\u627e\u5230\u5165\u53e3\u3002\u8bf7\u5148\u5728 OpenCode \u5de6\u4fa7\u9009\u4e2d\u9879\u76ee\uff0c\u518d\u627e\u9879\u76ee\u9875\u9762\u4e2d\u7684\u201c\u65b0\u5efa\u4f1a\u8bdd\u201d\u6216 + \u56fe\u6807\u3002";
        return;
      }
      var rect = activeTarget.getBoundingClientRect();
      spotlight.hidden = false;
      spotlight.style.left = Math.max(0, rect.left - 5) + "px";
      spotlight.style.top = Math.max(0, rect.top - 5) + "px";
      spotlight.style.width = rect.width + 10 + "px";
      spotlight.style.height = rect.height + 10 + "px";
      pointer.hidden = false;
      pointer.textContent = activeKind === "project" ? "\u70b9\u8fd9\u91cc\u6dfb\u52a0\u6587\u4ef6\u5939\uff0c\u7136\u540e\u5728\u5de6\u4fa7\u9009\u4e2d\u9879\u76ee" : "\u70b9\u8fd9\u91cc\u521b\u5efa\u4f1a\u8bdd";
      pointer.style.left = Math.max(8, Math.min(rect.left, window.innerWidth - 250)) + "px";
      pointer.style.top = (rect.bottom + 12 + 55 < window.innerHeight ? rect.bottom + 12 : Math.max(8, rect.top - 60)) + "px";
      status.textContent = "\u5df2\u5728 OpenCode \u754c\u9762\u4e2d\u6807\u51fa\u5165\u53e3\uff1b\u8bf7\u76f4\u63a5\u70b9\u51fb\u9ad8\u4eae\u6309\u94ae\u3002";
    }

    function queueSpotlightUpdate() {
      if (!updateQueued && activeKind) {
        updateQueued = true;
        window.requestAnimationFrame(updateSpotlight);
      }
    }

    function setOpen(open, moveFocus) {
      panel.hidden = !open;
      help.hidden = open;
      if (!open) clearSpotlight();
      if (open && moveFocus) window.setTimeout(function () { close.focus(); }, 0);
    }

    function closeAndRemember() {
      rememberDismissal();
      setOpen(false, false);
      help.focus();
    }

    function showCreateStatus(message, tone) {
      createStatus.textContent = message;
      if (tone) createStatus.setAttribute("data-tone", tone);
      else createStatus.removeAttribute("data-tone");
    }

    if (!canCreateProject) {
      createForm.hidden = true;
      createCopy.textContent = "\u5f53\u524d\u94fe\u63a5\u6ca1\u6709\u521b\u5efa\u6587\u4ef6\u5939\u7684\u6743\u9650\uff0c\u8bf7\u8ba9\u5b9e\u4f8b\u6240\u6709\u8005\u5148\u51c6\u5907\u9879\u76ee\u76ee\u5f55\u3002";
    }

    createForm.addEventListener("submit", async function (event) {
      event.preventDefault();
      if (!canCreateProject || createButton.disabled) return;
      var name = createInput.value.trim();
      if (!name) {
        showCreateStatus("\u8bf7\u8f93\u5165\u6587\u4ef6\u5939\u540d\u79f0\u3002", "error");
        createInput.focus();
        return;
      }

      createButton.disabled = true;
      createInput.disabled = true;
      showCreateStatus("\u6b63\u5728\u521b\u5efa\u2026", "");
      try {
        var response = await window.fetch(createProjectPath, {
          method: "POST",
          credentials: "same-origin",
          headers: {
            "Content-Type": "application/json",
            "X-ClawManager-Onboarding": "1"
          },
          body: JSON.stringify({ name: name })
        });
        var payload = await response.json().catch(function () { return {}; });
        if (response.ok) {
          var createdName = payload && payload.data && payload.data.name ? payload.data.name : name;
          var createdPath = payload && payload.data && payload.data.path ? payload.data.path : createdName;
          showCreateStatus("\u6587\u4ef6\u5939\u201c" + createdName + "\u201d\u5df2\u521b\u5efa\uff08" + createdPath + "\uff09\u3002\u70b9\u51fb\u4e0b\u65b9\u201c\u5e26\u6211\u627e\u5230\u6dfb\u52a0\u9879\u76ee\u201d\u7ee7\u7eed\u3002", "success");
          return;
        }
        if (response.status === 409 && payload.code === "PROJECT_EXISTS") {
          showCreateStatus("\u8be5\u6587\u4ef6\u5939\u5df2\u5b58\u5728\uff0c\u53ef\u4ee5\u76f4\u63a5\u70b9\u51fb\u5de6\u4fa7\u201c\u6dfb\u52a0\u9879\u76ee\u201d\u9009\u62e9\u5b83\u3002", "success");
          return;
        }
        if (response.status === 409) {
          showCreateStatus("\u5f53\u524d\u5de5\u4f5c\u533a\u6682\u4e0d\u53ef\u7528\uff0c\u8bf7\u7b49\u5f85\u5b9e\u4f8b\u542f\u52a8\u6216\u66f4\u65b0\u5b8c\u6210\u540e\u91cd\u8bd5\u3002", "error");
          return;
        }
        if (response.status === 403) {
          canCreateProject = false;
          createForm.hidden = true;
          createCopy.textContent = "\u5f53\u524d\u94fe\u63a5\u6ca1\u6709\u521b\u5efa\u6587\u4ef6\u5939\u7684\u6743\u9650\uff0c\u8bf7\u8ba9\u5b9e\u4f8b\u6240\u6709\u8005\u5148\u51c6\u5907\u9879\u76ee\u76ee\u5f55\u3002";
          return;
        }
        showCreateStatus("\u521b\u5efa\u5931\u8d25\uff0c\u8bf7\u68c0\u67e5\u6587\u4ef6\u5939\u540d\u79f0\u540e\u91cd\u8bd5\u3002", "error");
      } catch (_) {
        showCreateStatus("\u521b\u5efa\u5931\u8d25\uff0c\u8bf7\u7a0d\u540e\u91cd\u8bd5\u3002", "error");
      } finally {
        if (canCreateProject && createStatus.getAttribute("data-tone") !== "success") {
          createButton.disabled = false;
          createInput.disabled = false;
        }
      }
    });

    root.querySelectorAll(".cm-oc-locate").forEach(function (button) {
      button.addEventListener("click", function () {
        activeKind = button.getAttribute("data-target");
        updateSpotlight();
      });
    });
    window.addEventListener("resize", queueSpotlightUpdate);
    window.addEventListener("scroll", queueSpotlightUpdate, true);
    document.addEventListener("click", function (event) {
      if (activeKind && activeTarget && event.composedPath().includes(activeTarget)) clearSpotlight();
    }, true);
    new MutationObserver(queueSpotlightUpdate).observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ["style", "class", "aria-label", "title"] });

    close.addEventListener("click", closeAndRemember);
    acknowledge.addEventListener("click", closeAndRemember);
    help.addEventListener("click", function () { setOpen(true, true); });
    panel.addEventListener("keydown", function (event) {
      if (event.key === "Escape") closeAndRemember();
    });

    document.body.appendChild(host);
    setOpen(!dismissed(), false);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", mount, { once: true });
  } else {
    window.setTimeout(mount, 0);
  }
})();
