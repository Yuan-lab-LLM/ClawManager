import{f as e,r as t,u as n}from"./i18n-B06L7jQN.js";import{t as r}from"./format-Dg6tFpW6.js";import{t as i}from"./string-coerce-BcFtIWA_.js";import{n as a,r as o}from"./open-external-url-DHNx0AO6.js";import{i as s,n as c,r as l,t as u}from"./skills-shared-D6eRDyeb.js";function d(e){return e?a(e,window.location.href):null}var f=[{id:`all`,label:`全部`},{id:`ready`,label:`就绪`},{id:`needs-setup`,label:`需要设置`},{id:`disabled`,label:`已禁用`}];function p(e,t){switch(t){case`all`:return!0;case`ready`:return!e.disabled&&e.eligible;case`needs-setup`:return!e.disabled&&!e.eligible;case`disabled`:return e.disabled}throw Error(`Unsupported skills status filter`)}function m(e){return e.disabled?`muted`:e.eligible?`ok`:`warn`}function h(r){let a=r.report?.skills??[],o={all:a.length,ready:0,"needs-setup":0,disabled:0};for(let e of a)e.disabled?o.disabled++:e.eligible?o.ready++:o[`needs-setup`]++;let c=r.statusFilter===`all`?a:a.filter(e=>p(e,r.statusFilter)),l=i(r.filter),u=l?c.filter(e=>i([e.name,e.description,e.source].join(` `)).includes(l)):c,d=s(u),m=r.detailKey?a.find(e=>e.skillKey===r.detailKey)??null:null;return e`
    <section class="card">
      <div class="row" style="justify-content: space-between;">
        <div>
          <div class="card-title">技能</div>
          <div class="card-sub">已安装技能及其状态。</div>
        </div>
        <button
          class="btn"
          ?disabled=${r.loading||!r.connected}
          @click=${r.onRefresh}
        >
          ${r.loading?t(`common.loading`):t(`common.refresh`)}
        </button>
      </div>

      <div class="agent-tabs" style="margin-top: 14px;">
        ${f.map(t=>e`
            <button
              class="agent-tab ${r.statusFilter===t.id?`active`:``}"
              @click=${()=>r.onStatusFilterChange(t.id)}
            >
              ${t.label}<span class="agent-tab-count">${o[t.id]}</span>
            </button>
          `)}
      </div>

      <div
        class="filters"
        style="display: flex; align-items: center; gap: 12px; flex-wrap: wrap; margin-top: 12px;"
      >
        <label class="field" style="flex: 1; min-width: 180px;">
          <input
            .value=${r.filter}
            @input=${e=>r.onFilterChange(e.target.value)}
            placeholder="筛选已安装技能"
            autocomplete="off"
            name="skills-filter"
          />
        </label>
        <div class="muted">已显示 ${u.length} 项</div>
      </div>

      <div style="margin-top: 16px; border-top: 1px solid var(--border); padding-top: 16px;">
        <div style="display: flex; align-items: center; gap: 12px; margin-bottom: 12px;">
          <div style="font-weight: 600;">ClawHub</div>
          <div class="muted" style="font-size: 13px;">
            从注册表搜索并安装技能
          </div>
        </div>
        <div style="display: flex; align-items: center; gap: 12px; flex-wrap: wrap;">
          <label class="field" style="flex: 1; min-width: 180px;">
            <input
              .value=${r.clawhubQuery}
              @input=${e=>r.onClawHubQueryChange(e.target.value)}
              placeholder="搜索 ClawHub 技能…"
              autocomplete="off"
              name="clawhub-search"
            />
          </label>
          ${r.clawhubSearchLoading?e`<span class="muted">搜索中…</span>`:n}
        </div>
        ${r.clawhubSearchError?e`<div class="callout danger" style="margin-top: 8px;">
              ${r.clawhubSearchError}
            </div>`:n}
        ${r.clawhubInstallMessage?e`<div
              class="callout ${r.clawhubInstallMessage.kind===`error`?`danger`:`success`}"
              style="margin-top: 8px;"
            >
              ${r.clawhubInstallMessage.text}
            </div>`:n}
        ${g(r)}
      </div>

      ${r.error?e`<div class="callout danger" style="margin-top: 12px;">${r.error}</div>`:n}
      ${u.length===0?e`
            <div class="muted" style="margin-top: 16px">
              ${!r.connected&&!r.report?`Not connected to gateway.`:`No skills found.`}
            </div>
          `:e`
            <div class="agent-skills-groups" style="margin-top: 16px;">
              ${d.map(t=>e`
                  <details class="agent-skills-group" open>
                    <summary class="agent-skills-header">
                      <span>${t.label}</span>
                      <span class="muted">${t.skills.length}</span>
                    </summary>
                    <div class="list skills-grid">
                      ${t.skills.map(e=>v(e,r))}
                    </div>
                  </details>
                `)}
            </div>
          `}
    </section>

    ${m?y(m,r):n}
    ${r.clawhubDetailSlug?_(r):n}
  `}function g(t){let i=t.clawhubResults;return i?i.length===0?e`<div class="muted" style="margin-top: 8px;">No skills found on ClawHub.</div>`:e`
    <div class="list" style="margin-top: 8px;">
      ${i.map(i=>e`
          <div
            class="list-item list-item-clickable"
            @click=${()=>t.onClawHubDetailOpen(i.slug)}
          >
            <div class="list-main">
              <div class="list-title">${i.displayName}</div>
              <div class="list-sub">${i.summary?r(i.summary,120):i.slug}</div>
            </div>
            <div class="list-meta" style="display: flex; align-items: center; gap: 8px;">
              ${i.version?e`<span class="muted" style="font-size: 12px;">v${i.version}</span>`:n}
              <button
                class="btn btn--sm"
                ?disabled=${t.clawhubInstallSlug!==null}
                @click=${e=>{e.stopPropagation(),t.onClawHubInstall(i.slug)}}
              >
                ${t.clawhubInstallSlug===i.slug?`安装中…`:`安装`}
              </button>
            </div>
          </div>
        `)}
    </div>
  `:n}function _(r){let i=r.clawhubDetail;return e`
    <dialog
      class="md-preview-dialog"
      ${o(e=>{!(e instanceof HTMLDialogElement)||e.open||e.showModal()})}
      @click=${e=>{let t=e.currentTarget;e.target===t&&t.close()}}
      @close=${r.onClawHubDetailClose}
    >
      <div class="md-preview-dialog__panel">
        <div class="md-preview-dialog__header">
          <div class="md-preview-dialog__title">
            ${i?.skill?.displayName??r.clawhubDetailSlug}
          </div>
          <button
            class="btn btn--sm"
            @click=${e=>{e.currentTarget.closest(`dialog`)?.close()}}
          >
            关闭
          </button>
        </div>
        <div class="md-preview-dialog__body" style="display: grid; gap: 16px;">
          ${r.clawhubDetailLoading?e`<div class="muted">${t(`common.loading`)}</div>`:r.clawhubDetailError?e`<div class="callout danger">${r.clawhubDetailError}</div>`:i?.skill?e`
                    <div style="font-size: 14px; line-height: 1.5;">
                      ${i.skill.summary??``}
                    </div>
                    ${i.owner?.displayName?e`<div class="muted" style="font-size: 13px;">
                          By
                          ${i.owner.displayName}${i.owner.handle?e` (@${i.owner.handle})`:n}
                        </div>`:n}
                    ${i.latestVersion?e`<div class="muted" style="font-size: 13px;">
                          Latest: v${i.latestVersion.version}
                        </div>`:n}
                    ${i.latestVersion?.changelog?e`<div
                          style="font-size: 13px; border-top: 1px solid var(--border); padding-top: 12px; white-space: pre-wrap;"
                        >
                          ${i.latestVersion.changelog}
                        </div>`:n}
                    ${i.metadata?.os?e`<div class="muted" style="font-size: 12px;">
                          Platforms: ${i.metadata.os.join(`, `)}
                        </div>`:n}
                    <button
                      class="btn primary"
                      ?disabled=${r.clawhubInstallSlug!==null}
                      @click=${()=>{r.clawhubDetailSlug&&r.onClawHubInstall(r.clawhubDetailSlug)}}
                    >
                      ${r.clawhubInstallSlug===r.clawhubDetailSlug?`安装中…`:`安装 ${i.skill.displayName}`}
                    </button>
                  `:e`<div class="muted">未找到技能。</div>`}
        </div>
      </div>
    </dialog>
  `}function v(t,i){let a=i.busyKey===t.skillKey;return e`
    <div class="list-item list-item-clickable" @click=${()=>i.onDetailOpen(t.skillKey)}>
      <div class="list-main">
        <div class="list-title" style="display: flex; align-items: center; gap: 8px;">
          <span class="statusDot ${m(t)}"></span>
          ${t.emoji?e`<span>${t.emoji}</span>`:n}
          <span>${t.name}</span>
        </div>
        <div class="list-sub">${r(t.description,140)}</div>
      </div>
      <div
        class="list-meta"
        style="display: flex; align-items: center; justify-content: flex-end; gap: 10px;"
      >
        <label class="skill-toggle-wrap" @click=${e=>e.stopPropagation()}>
          <input
            type="checkbox"
            class="skill-toggle"
            .checked=${!t.disabled}
            ?disabled=${a}
            @change=${e=>{e.stopPropagation(),i.onToggle(t.skillKey,t.disabled)}}
          />
        </label>
      </div>
    </div>
  `}function y(t,r){let i=r.busyKey===t.skillKey,a=r.edits[t.skillKey]??``,s=r.messages[t.skillKey]??null,f=t.install.length>0&&t.missing.bins.length>0,p=!!(t.bundled&&t.source!==`openclaw-bundled`),h=u(t),g=c(t);return e`
    <dialog
      class="md-preview-dialog"
      ${o(e=>{!(e instanceof HTMLDialogElement)||e.open||e.showModal()})}
      @click=${e=>{let t=e.currentTarget;e.target===t&&t.close()}}
      @close=${r.onDetailClose}
    >
      <div class="md-preview-dialog__panel">
        <div class="md-preview-dialog__header">
          <div
            class="md-preview-dialog__title"
            style="display: flex; align-items: center; gap: 8px;"
          >
            <span class="statusDot ${m(t)}"></span>
            ${t.emoji?e`<span style="font-size: 18px;">${t.emoji}</span>`:n}
            <span>${t.name}</span>
          </div>
          <button
            class="btn btn--sm"
            @click=${e=>{e.currentTarget.closest(`dialog`)?.close()}}
          >
            关闭
          </button>
        </div>
        <div class="md-preview-dialog__body" style="display: grid; gap: 16px;">
          <div>
            <div style="font-size: 14px; line-height: 1.5; color: var(--text);">
              ${t.description}
            </div>
            ${l({skill:t,showBundledBadge:p})}
          </div>

          ${h.length>0?e`
                <div
                  class="callout"
                  style="border-color: var(--warn-subtle); background: var(--warn-subtle); color: var(--warn);"
                >
                  <div style="font-weight: 600; margin-bottom: 4px;">Missing requirements</div>
                  <div>${h.join(`, `)}</div>
                </div>
              `:n}
          ${g.length>0?e`
                <div class="muted" style="font-size: 13px;">Reason: ${g.join(`, `)}</div>
              `:n}

          <div style="display: flex; align-items: center; gap: 12px;">
            <label class="skill-toggle-wrap">
              <input
                type="checkbox"
                class="skill-toggle"
                .checked=${!t.disabled}
                ?disabled=${i}
                @change=${()=>r.onToggle(t.skillKey,t.disabled)}
              />
            </label>
            <span style="font-size: 13px; font-weight: 500;">
              ${t.disabled?`已禁用`:`已启用`}
            </span>
            ${f?e`<button
                  class="btn"
                  ?disabled=${i}
                  @click=${()=>r.onInstall(t.skillKey,t.name,t.install[0].id)}
                >
                  ${i?`安装中…`:t.install[0].label}
                </button>`:n}
          </div>

          ${s?e`<div class="callout ${s.kind===`error`?`danger`:`success`}">
                ${s.message}
              </div>`:n}
          ${t.primaryEnv?e`
                <div style="display: grid; gap: 8px;">
                  <div class="field">
                    <span
                      >API key
                      <span class="muted" style="font-weight: normal; font-size: 0.88em;"
                        >(${t.primaryEnv})</span
                      ></span
                    >
                    <input
                      type="password"
                      .value=${a}
                      @input=${e=>r.onEdit(t.skillKey,e.target.value)}
                    />
                  </div>
                  ${(()=>{let r=d(t.homepage);return r?e`<div class="muted" style="font-size: 13px;">
                          Get your key:
                          <a href="${r}" target="_blank" rel="noopener noreferrer"
                            >${t.homepage}</a
                          >
                        </div>`:n})()}
                  <button
                    class="btn primary"
                    ?disabled=${i}
                    @click=${()=>r.onSaveKey(t.skillKey)}
                  >
                    Save key
                  </button>
                </div>
              `:n}

          <div
            style="border-top: 1px solid var(--border); padding-top: 12px; display: grid; gap: 6px; font-size: 12px; color: var(--muted);"
          >
            <div><span style="font-weight: 600;">Source:</span> ${t.source}</div>
            <div style="font-family: var(--mono); word-break: break-all;">${t.filePath}</div>
            ${(()=>{let r=d(t.homepage);return r?e`<div>
                    <a href="${r}" target="_blank" rel="noopener noreferrer"
                      >${t.homepage}</a
                    >
                  </div>`:n})()}
          </div>
        </div>
      </div>
    </dialog>
  `}export{h as renderSkills};
//# sourceMappingURL=skills-BRWdbtpV.js.map