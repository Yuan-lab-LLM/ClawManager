import{f as e,r as t,u as n}from"./i18n-B06L7jQN.js";import{o as r}from"./format-Dg6tFpW6.js";import{n as i,t as a}from"./string-coerce-BcFtIWA_.js";import{t as o}from"./icons-CRpuKbeA.js";function s(e){if(e)return Array.isArray(e.type)?e.type.find(e=>e!==`null`)??e.type[0]:e.type}function c(e){if(!e)return``;if(e.default!==void 0)return e.default;switch(s(e)){case`object`:return{};case`array`:return[];case`boolean`:return!1;case`number`:case`integer`:return 0;case`string`:return``;default:return``}}function l(e){return e.filter(e=>typeof e==`string`).join(`.`)}function u(e,t){let n=l(e),r=t[n];if(r)return r;let i=n.split(`.`);for(let[e,n]of Object.entries(t)){if(!e.includes(`*`))continue;let t=e.split(`.`);if(t.length!==i.length)continue;let r=!0;for(let e=0;e<i.length;e+=1)if(t[e]!==`*`&&t[e]!==i[e]){r=!1;break}if(r)return n}}function d(e){return e.replace(/_/g,` `).replace(/([a-z0-9])([A-Z])/g,`$1 $2`).replace(/\s+/g,` `).replace(/^./,e=>e.toUpperCase())}var f=[`maxtokens`,`maxoutputtokens`,`maxinputtokens`,`maxcompletiontokens`,`contexttokens`,`totaltokens`,`tokencount`,`tokenlimit`,`tokenbudget`,`passwordfile`],p=[/token$/i,/password/i,/secret/i,/api.?key/i,/serviceaccount(?:ref)?$/i],m=/^\$\{[^}]*\}$/,h=`[redacted - click reveal to view]`;function g(e){return m.test(e.trim())}function _(e){let t=a(e);return!f.some(e=>t.endsWith(e))&&p.some(t=>t.test(e))}function v(e){return typeof e==`string`?e.trim().length>0&&!g(e):e!=null}function y(e){return e?.sensitive??!1}function b(e,t,n){let r=l(t);return(y(u(t,n))||_(r))&&v(e)?!0:Array.isArray(e)?e.some((e,r)=>b(e,[...t,r],n)):e&&typeof e==`object`?Object.entries(e).some(([e,r])=>b(r,[...t,e],n)):!1}function x(e,t,n){if(e==null)return 0;let r=l(t);return(y(u(t,n))||_(r))&&v(e)?1:Array.isArray(e)?e.reduce((e,r,i)=>e+x(r,[...t,i],n),0):e&&typeof e==`object`?Object.entries(e).reduce((e,[r,i])=>e+x(i,[...t,r],n),0):0}function S(e){let{values:t,original:n}=e;return t.name!==n.name||t.displayName!==n.displayName||t.about!==n.about||t.picture!==n.picture||t.banner!==n.banner||t.website!==n.website||t.nip05!==n.nip05||t.lud16!==n.lud16}function C(r){let{state:i,callbacks:a,accountId:o}=r,s=S(i),c=(t,r,o={})=>{let{type:s=`text`,placeholder:c,maxLength:l,help:u}=o,d=i.values[t]??``,f=i.fieldErrors[t],p=`nostr-profile-${t}`;return s===`textarea`?e`
        <div class="form-field" style="margin-bottom: 12px;">
          <label for="${p}" style="display: block; margin-bottom: 4px; font-weight: 500;">
            ${r}
          </label>
          <textarea
            id="${p}"
            .value=${d}
            placeholder=${c??``}
            maxlength=${l??2e3}
            rows="3"
            style="width: 100%; padding: 8px; border: 1px solid var(--border-color); border-radius: var(--radius-sm); resize: vertical; font-family: inherit;"
            @input=${e=>{let n=e.target;a.onFieldChange(t,n.value)}}
            ?disabled=${i.saving}
          ></textarea>
          ${u?e`<div style="font-size: 12px; color: var(--text-muted); margin-top: 2px;">
                ${u}
              </div>`:n}
          ${f?e`<div style="font-size: 12px; color: var(--danger-color); margin-top: 2px;">
                ${f}
              </div>`:n}
        </div>
      `:e`
      <div class="form-field" style="margin-bottom: 12px;">
        <label for="${p}" style="display: block; margin-bottom: 4px; font-weight: 500;">
          ${r}
        </label>
        <input
          id="${p}"
          type=${s}
          .value=${d}
          placeholder=${c??``}
          maxlength=${l??256}
          style="width: 100%; padding: 8px; border: 1px solid var(--border-color); border-radius: var(--radius-sm);"
          @input=${e=>{let n=e.target;a.onFieldChange(t,n.value)}}
          ?disabled=${i.saving}
        />
        ${u?e`<div style="font-size: 12px; color: var(--text-muted); margin-top: 2px;">
              ${u}
            </div>`:n}
        ${f?e`<div style="font-size: 12px; color: var(--danger-color); margin-top: 2px;">
              ${f}
            </div>`:n}
      </div>
    `};return e`
    <div
      class="nostr-profile-form"
      style="padding: 16px; background: var(--bg-secondary); border-radius: var(--radius-md); margin-top: 12px;"
    >
      <div
        style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px;"
      >
        <div style="font-weight: 600; font-size: 16px;">${t(`channels.nostr.editProfile`)}</div>
        <div style="font-size: 12px; color: var(--text-muted);">
          ${t(`channels.nostr.account`)}: ${o}
        </div>
      </div>

      ${i.error?e`<div class="callout danger" style="margin-bottom: 12px;">${i.error}</div>`:n}
      ${i.success?e`<div class="callout success" style="margin-bottom: 12px;">${i.success}</div>`:n}
      ${(()=>{let r=i.values.picture;return r?e`
      <div style="margin-bottom: 12px;">
        <img
          src=${r}
          alt=${t(`channels.nostr.profilePicturePreview`)}
          style="max-width: 80px; max-height: 80px; border-radius: 50%; object-fit: cover; border: 2px solid var(--border-color);"
          @error=${e=>{let t=e.target;t.style.display=`none`}}
          @load=${e=>{let t=e.target;t.style.display=`block`}}
        />
      </div>
    `:n})()}
      ${c(`name`,t(`channels.nostr.username`),{placeholder:`satoshi`,maxLength:256,help:t(`channels.nostr.usernameHelp`)})}
      ${c(`displayName`,t(`channels.nostr.displayName`),{placeholder:`Satoshi Nakamoto`,maxLength:256,help:t(`channels.nostr.displayNameHelp`)})}
      ${c(`about`,t(`channels.nostr.bio`),{type:`textarea`,placeholder:t(`channels.nostr.bioPlaceholder`),maxLength:2e3,help:t(`channels.nostr.bioHelp`)})}
      ${c(`picture`,t(`channels.nostr.avatarUrl`),{type:`url`,placeholder:`https://example.com/avatar.jpg`,help:t(`channels.nostr.avatarHelp`)})}
      ${i.showAdvanced?e`
            <div
              style="border-top: 1px solid var(--border-color); padding-top: 12px; margin-top: 12px;"
            >
              <div style="font-weight: 500; margin-bottom: 12px; color: var(--text-muted);">
                ${t(`channels.nostr.advanced`)}
              </div>

              ${c(`banner`,t(`channels.nostr.bannerUrl`),{type:`url`,placeholder:`https://example.com/banner.jpg`,help:t(`channels.nostr.bannerHelp`)})}
              ${c(`website`,t(`channels.nostr.website`),{type:`url`,placeholder:`https://example.com`,help:t(`channels.nostr.websiteHelp`)})}
              ${c(`nip05`,t(`channels.nostr.nip05Identifier`),{placeholder:`you@example.com`,help:t(`channels.nostr.nip05Help`)})}
              ${c(`lud16`,t(`channels.nostr.lightningAddress`),{placeholder:`you@getalby.com`,help:t(`channels.nostr.lightningHelp`)})}
            </div>
          `:n}

      <div style="display: flex; gap: 8px; margin-top: 16px; flex-wrap: wrap;">
        <button
          class="btn primary"
          @click=${a.onSave}
          ?disabled=${i.saving||!s}
        >
          ${i.saving?t(`common.saving`):t(`common.saveAndPublish`)}
        </button>

        <button
          class="btn"
          @click=${a.onImport}
          ?disabled=${i.importing||i.saving}
        >
          ${i.importing?t(`common.importing`):t(`common.importFromRelays`)}
        </button>

        <button class="btn" @click=${a.onToggleAdvanced}>
          ${i.showAdvanced?t(`common.hideAdvanced`):t(`common.showAdvanced`)}
        </button>

        <button class="btn" @click=${a.onCancel} ?disabled=${i.saving}>
          ${t(`common.cancel`)}
        </button>
      </div>

      ${s?e`
            <div style="font-size: 12px; color: var(--warning-color); margin-top: 8px">
              ${t(`common.unsavedChanges`)}
            </div>
          `:n}
    </div>
  `}function w(e){let t={name:e?.name??``,displayName:e?.displayName??``,about:e?.about??``,picture:e?.picture??``,banner:e?.banner??``,website:e?.website??``,nip05:e?.nip05??``,lud16:e?.lud16??``};return{values:t,original:{...t},saving:!1,importing:!1,error:null,success:null,fieldErrors:{},showAdvanced:!!(e?.banner||e?.website||e?.nip05||e?.lud16)}}var T=new Set([`title`,`description`,`default`,`nullable`,`tags`,`x-tags`]);function E(e){return Object.keys(e??{}).filter(e=>!T.has(e)).length===0}function D(e){if(e===void 0)return``;try{return JSON.stringify(e,null,2)??``}catch{return``}}function O(e){return typeof e==`string`||typeof e==`number`||typeof e==`boolean`||typeof e==`bigint`?String(e):null}function k(e,t){if(Object.is(e,t))return!0;let n=O(e),r=O(t);return n!==null&&n===r}var A={chevronDown:e`
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
    >
      <polyline points="6 9 12 15 18 9"></polyline>
    </svg>
  `,plus:e`
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
    >
      <line x1="12" y1="5" x2="12" y2="19"></line>
      <line x1="5" y1="12" x2="19" y2="12"></line>
    </svg>
  `,minus:e`
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
    >
      <line x1="5" y1="12" x2="19" y2="12"></line>
    </svg>
  `,trash:e`
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
    >
      <polyline points="3 6 5 6 21 6"></polyline>
      <path
        d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"
      ></path>
    </svg>
  `,edit:e`
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
    >
      <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"></path>
      <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"></path>
    </svg>
  `};function j(e){if(!e||typeof e!=`object`||Array.isArray(e))return!1;let t=e;return typeof t.source!=`string`||typeof t.id!=`string`?!1:t.provider===void 0||typeof t.provider==`string`}function M(e){let t=b(e.value,e.path,e.hints),n=t&&(e.revealSensitive||(e.isSensitivePathRevealed?.(e.path)??!1));return{isSensitive:t,isRedacted:t&&!n,isRevealed:n,canReveal:t}}function N(t){let{state:r}=t;return!r.isSensitive||!t.onToggleSensitivePath?n:e`
    <button
      type="button"
      class="btn btn--icon ${r.isRevealed?`active`:``}"
      style="width:28px;height:28px;padding:0;"
      title=${r.canReveal?r.isRevealed?`Hide value`:`Reveal value`:`Disable stream mode to reveal value`}
      aria-label=${r.canReveal?r.isRevealed?`Hide value`:`Reveal value`:`Disable stream mode to reveal value`}
      aria-pressed=${r.isRevealed}
      ?disabled=${t.disabled||!r.canReveal}
      @click=${()=>t.onToggleSensitivePath?.(t.path)}
    >
      ${r.isRevealed?o.eye:o.eyeOff}
    </button>
  `}function P(e){return!!(e&&(e.text.length>0||e.tags.length>0))}function F(e){let t=[],n=new Set;return{text:a(e.trim().replace(/(^|\s)tag:([^\s]+)/gi,(e,r,i)=>{let o=a(i);return o&&!n.has(o)&&(n.add(o),t.push(o)),r})),tags:t}}function I(e){if(!Array.isArray(e))return[];let t=new Set,n=[];for(let r of e){if(typeof r!=`string`)continue;let e=r.trim();if(!e)continue;let i=a(e);t.has(i)||(t.add(i),n.push(e))}return n}function L(e,t,n){let r=u(e,n),i=r?.label??t.title??d(String(e.at(-1))),a=r?.help??t.description,o=I(t[`x-tags`]??t.tags),s=I(r?.tags);return{label:i,help:a,tags:s.length>0?s:o}}function R(e,t){if(!e)return!0;for(let n of t)if(i(n)?.includes(e))return!0;return!1}function ee(e,t){if(e.length===0)return!0;let n=new Set(t.map(e=>a(e)));return e.every(e=>n.has(e))}function z(e){let{schema:t,path:n,hints:r,criteria:i}=e;if(!P(i))return!0;let{label:a,help:o,tags:s}=L(n,t,r);if(!ee(i.tags,s))return!1;if(!i.text)return!0;let c=n.filter(e=>typeof e==`string`).join(`.`),l=t.enum&&t.enum.length>0?t.enum.map(e=>String(e)).join(` `):``;return R(i.text,[a,o,t.title,t.description,c,l])}function B(e){let{schema:t,value:n,path:r,hints:i,criteria:a}=e;if(!P(a)||z({schema:t,path:r,hints:i,criteria:a}))return!0;let o=s(t);if(o===`object`){let e=n??t.default,o=e&&typeof e==`object`&&!Array.isArray(e)?e:{},s=t.properties??{};for(let[e,t]of Object.entries(s))if(B({schema:t,value:o[e],path:[...r,e],hints:i,criteria:a}))return!0;let c=t.additionalProperties;if(c&&typeof c==`object`){let e=new Set(Object.keys(s));for(let[t,n]of Object.entries(o))if(!e.has(t)&&B({schema:c,value:n,path:[...r,t],hints:i,criteria:a}))return!0}return!1}if(o===`array`){let e=Array.isArray(t.items)?t.items[0]:t.items;if(!e)return!1;let o=Array.isArray(n)?n:Array.isArray(t.default)?t.default:[];if(o.length===0)return!1;for(let t=0;t<o.length;t+=1)if(B({schema:e,value:o[t],path:[...r,t],hints:i,criteria:a}))return!0}return!1}function V(t){return t.length===0?n:e`
    <div class="cfg-tags">${t.map(t=>e`<span class="cfg-tag">${t}</span>`)}</div>
  `}function H(t){let{schema:i,value:a,path:o,hints:c,unsupported:u,disabled:d,onPatch:f}=t,p=t.showLabel??!0,m=s(i),{label:h,help:g,tags:_}=L(o,i,c),v=l(o),y=t.searchCriteria;if(u.has(v))return e`<div class="cfg-field cfg-field--error">
      <div class="cfg-field__label">${h}</div>
      <div class="cfg-field__error">不支持的架构节点。请使用原始模式。</div>
    </div>`;if(y&&P(y)&&!B({schema:i,value:a,path:o,hints:c,criteria:y}))return n;if(i.anyOf||i.oneOf){let l=(i.anyOf??i.oneOf??[]).filter(e=>!(e.type===`null`||Array.isArray(e.type)&&e.type.includes(`null`)));if(l.length===1)return H({...t,schema:l[0]});let u=l.map(e=>{if(e.const!==void 0)return e.const;if(e.enum&&e.enum.length===1)return e.enum[0]}),m=u.every(e=>e!==void 0);if(m&&u.length>0&&u.length<=5){let t=a??i.default;return e`
        <div class="cfg-field">
          ${p?e`<label class="cfg-field__label">${h}</label>`:n}
          ${g?e`<div class="cfg-field__help">${g}</div>`:n} ${V(_)}
          <div class="cfg-segmented">
            ${u.map(n=>e`
                <button
                  type="button"
                  class="cfg-segmented__btn ${k(n,t)?`active`:``}"
                  ?disabled=${d}
                  @click=${()=>f(o,n)}
                >
                  ${r(n)}
                </button>
              `)}
          </div>
        </div>
      `}if(m&&u.length>5)return G({...t,options:u,value:a??i.default});let v=new Set(l.map(e=>s(e)).filter(Boolean)),y=new Set([...v].map(e=>e===`integer`?`number`:e));if([...y].every(e=>[`string`,`number`,`boolean`].includes(e))){let e=y.has(`string`),n=y.has(`number`);if(y.has(`boolean`)&&y.size===1)return H({...t,schema:{...i,type:`boolean`,anyOf:void 0,oneOf:void 0}});if(e||n)return U({...t,inputType:n&&!e?`number`:`text`})}return K({schema:i,value:a,path:o,hints:c,disabled:d,showLabel:p,revealSensitive:t.revealSensitive??!1,isSensitivePathRevealed:t.isSensitivePathRevealed,onToggleSensitivePath:t.onToggleSensitivePath,onPatch:f})}if(i.enum){let s=i.enum;if(s.length<=5){let t=a??i.default;return e`
        <div class="cfg-field">
          ${p?e`<label class="cfg-field__label">${h}</label>`:n}
          ${g?e`<div class="cfg-field__help">${g}</div>`:n} ${V(_)}
          <div class="cfg-segmented">
            ${s.map(n=>e`
                <button
                  type="button"
                  class="cfg-segmented__btn ${k(n,t)?`active`:``}"
                  ?disabled=${d}
                  @click=${()=>f(o,n)}
                >
                  ${r(n)}
                </button>
              `)}
          </div>
        </div>
      `}return G({...t,options:s,value:a??i.default})}if(m===`object`)return q(t);if(m===`array`)return J(t);if(m===`boolean`){let t=typeof a==`boolean`?a:typeof i.default==`boolean`?i.default:!1;return e`
      <label class="cfg-toggle-row ${d?`disabled`:``}">
        <div class="cfg-toggle-row__content">
          <span class="cfg-toggle-row__label">${h}</span>
          ${g?e`<span class="cfg-toggle-row__help">${g}</span>`:n}
          ${V(_)}
        </div>
        <div class="cfg-toggle">
          <input
            type="checkbox"
            .checked=${t}
            ?disabled=${d}
            @change=${e=>f(o,e.target.checked)}
          />
          <span class="cfg-toggle__track"></span>
        </div>
      </label>
    `}return m===`number`||m===`integer`?W(t):m===`string`?U({...t,inputType:`text`}):e`
    <div class="cfg-field cfg-field--error">
      <div class="cfg-field__label">${h}</div>
      <div class="cfg-field__error">不支持的类型：${m}。请使用原始模式。</div>
    </div>
  `}function U(t){let{schema:i,value:a,path:o,hints:s,disabled:c,onPatch:l,inputType:d}=t,f=t.showLabel??!0,p=u(o,s),{label:m,help:g,tags:_}=L(o,i,s),v=M({path:o,value:a,hints:s,revealSensitive:t.revealSensitive??!1,isSensitivePathRevealed:t.isSensitivePathRevealed}),y=typeof a==`object`&&!!a&&!Array.isArray(a),b=j(a),x=t.rawAvailable??!0,S=v.isRedacted||b,C=S?b?x?`结构化值（SecretRef）- 请使用原始模式编辑`:`结构化值（SecretRef）- 请直接编辑配置文件`:h:p?.placeholder??(i.default===void 0?``:`Default: ${r(i.default)}`),w=S?``:y?D(a):a??``,T=v.isSensitive&&!S?`text`:d;return e`
    <div class="cfg-field">
      ${f?e`<label class="cfg-field__label">${m}</label>`:n}
      ${g?e`<div class="cfg-field__help">${g}</div>`:n} ${V(_)}
      <div class="cfg-input-wrap">
        <input
          type=${T}
          class="cfg-input${S?` cfg-input--redacted`:``}"
          placeholder=${C}
          .value=${r(w)}
          ?disabled=${c}
          ?readonly=${S}
          @click=${()=>{v.isRedacted&&!b&&t.onToggleSensitivePath&&t.onToggleSensitivePath(o)}}
          @input=${e=>{if(S)return;let t=e.target.value;if(d===`number`){if(t.trim()===``){l(o,void 0);return}let e=Number(t);l(o,Number.isNaN(e)?t:e);return}l(o,t)}}
          @change=${e=>{if(d===`number`||S)return;let t=e.target.value;l(o,t.trim())}}
        />
        ${b?n:N({path:o,state:v,disabled:c,onToggleSensitivePath:t.onToggleSensitivePath})}
        ${i.default===void 0?n:e`
              <button
                type="button"
                class="cfg-input__reset"
                title="Reset to default"
                ?disabled=${c||S}
                @click=${()=>l(o,i.default)}
              >
                ↺
              </button>
            `}
      </div>
    </div>
  `}function W(t){let{schema:i,value:a,path:o,hints:s,disabled:c,onPatch:l}=t,u=t.showLabel??!0,{label:d,help:f,tags:p}=L(o,i,s),m=a??i.default??``,h=typeof m==`number`?m:0;return e`
    <div class="cfg-field">
      ${u?e`<label class="cfg-field__label">${d}</label>`:n}
      ${f?e`<div class="cfg-field__help">${f}</div>`:n} ${V(p)}
      <div class="cfg-number">
        <button
          type="button"
          class="cfg-number__btn"
          ?disabled=${c}
          @click=${()=>l(o,h-1)}
        >
          −
        </button>
        <input
          type="number"
          class="cfg-number__input"
          .value=${r(m)}
          ?disabled=${c}
          @input=${e=>{let t=e.target.value;l(o,t===``?void 0:Number(t))}}
        />
        <button
          type="button"
          class="cfg-number__btn"
          ?disabled=${c}
          @click=${()=>l(o,h+1)}
        >
          +
        </button>
      </div>
    </div>
  `}function G(t){let{schema:r,value:i,path:a,hints:o,disabled:s,options:c,onPatch:l}=t,u=t.showLabel??!0,{label:d,help:f,tags:p}=L(a,r,o),m=i??r.default,h=c.findIndex(e=>e===m||String(e)===String(m)),g=`__unset__`;return e`
    <div class="cfg-field">
      ${u?e`<label class="cfg-field__label">${d}</label>`:n}
      ${f?e`<div class="cfg-field__help">${f}</div>`:n} ${V(p)}
      <select
        class="cfg-select"
        ?disabled=${s}
        .value=${h>=0?String(h):g}
        @change=${e=>{let t=e.target.value;l(a,t===g?void 0:c[Number(t)])}}
      >
        <option value=${g}>Select...</option>
        ${c.map((t,n)=>e` <option value=${String(n)}>${String(t)}</option> `)}
      </select>
    </div>
  `}function K(t){let{schema:r,value:i,path:a,hints:o,disabled:s,onPatch:c}=t,l=t.showLabel??!0,{label:u,help:d,tags:f}=L(a,r,o),p=D(i),m=M({path:a,value:i,hints:o,revealSensitive:t.revealSensitive??!1,isSensitivePathRevealed:t.isSensitivePathRevealed}),g=m.isRedacted?``:p;return e`
    <div class="cfg-field">
      ${l?e`<label class="cfg-field__label">${u}</label>`:n}
      ${d?e`<div class="cfg-field__help">${d}</div>`:n} ${V(f)}
      <div class="cfg-input-wrap">
        <textarea
          class="cfg-textarea${m.isRedacted?` cfg-textarea--redacted`:``}"
          placeholder=${m.isRedacted?h:`JSON value`}
          rows="3"
          .value=${g}
          ?disabled=${s}
          ?readonly=${m.isRedacted}
          @click=${()=>{m.isRedacted&&t.onToggleSensitivePath&&t.onToggleSensitivePath(a)}}
          @change=${e=>{if(m.isRedacted)return;let t=e.target,n=t.value.trim();if(!n){c(a,void 0);return}try{c(a,JSON.parse(n))}catch{t.value=p}}}
        ></textarea>
        ${N({path:a,state:m,disabled:s,onToggleSensitivePath:t.onToggleSensitivePath})}
      </div>
    </div>
  `}function q(t){let{schema:r,value:i,path:a,hints:o,unsupported:s,disabled:c,onPatch:l,searchCriteria:d,rawAvailable:f,revealSensitive:p,isSensitivePathRevealed:m,onToggleSensitivePath:h}=t,g=t.showLabel??!0,{label:_,help:v,tags:y}=L(a,r,o),b=d&&P(d)&&z({schema:r,path:a,hints:o,criteria:d})?void 0:d,x=i??r.default,S=x&&typeof x==`object`&&!Array.isArray(x)?x:{},C=r.properties??{},w=Object.entries(C).toSorted((e,t)=>{let n=u([...a,e[0]],o)?.order??0,r=u([...a,t[0]],o)?.order??0;return n===r?e[0].localeCompare(t[0]):n-r}),T=new Set(Object.keys(C)),E=r.additionalProperties,D=!!E&&typeof E==`object`,O=e`
    ${w.map(([e,t])=>H({schema:t,value:S[e],path:[...a,e],hints:o,rawAvailable:f,unsupported:s,disabled:c,searchCriteria:b,revealSensitive:p,isSensitivePathRevealed:m,onToggleSensitivePath:h,onPatch:l}))}
    ${D?Y({schema:E,value:S,path:a,hints:o,rawAvailable:f,unsupported:s,disabled:c,reservedKeys:T,searchCriteria:b,revealSensitive:p,isSensitivePathRevealed:m,onToggleSensitivePath:h,onPatch:l}):n}
  `;return a.length===1?e` <div class="cfg-fields">${O}</div> `:g?e`
    <details class="cfg-object" ?open=${a.length<=2}>
      <summary class="cfg-object__header">
        <span class="cfg-object__title-wrap">
          <span class="cfg-object__title">${_}</span>
          ${V(y)}
        </span>
        <span class="cfg-object__chevron">${A.chevronDown}</span>
      </summary>
      ${v?e`<div class="cfg-object__help">${v}</div>`:n}
      <div class="cfg-object__content">${O}</div>
    </details>
  `:e` <div class="cfg-fields cfg-fields--inline">${O}</div> `}function J(t){let{schema:r,value:i,path:a,hints:o,unsupported:s,disabled:l,onPatch:u,searchCriteria:d,rawAvailable:f,revealSensitive:p,isSensitivePathRevealed:m,onToggleSensitivePath:h}=t,g=t.showLabel??!0,{label:_,help:v,tags:y}=L(a,r,o),b=d&&P(d)&&z({schema:r,path:a,hints:o,criteria:d})?void 0:d,x=Array.isArray(r.items)?r.items[0]:r.items;if(!x)return e`
      <div class="cfg-field cfg-field--error">
        <div class="cfg-field__label">${_}</div>
        <div class="cfg-field__error">不支持的数组架构。请使用原始模式。</div>
      </div>
    `;let S=Array.isArray(i)?i:Array.isArray(r.default)?r.default:[];return e`
    <div class="cfg-array">
      <div class="cfg-array__header">
        <div class="cfg-array__title">
          ${g?e`<span class="cfg-array__label">${_}</span>`:n}
          ${V(y)}
        </div>
        <span class="cfg-array__count">${S.length} item${S.length===1?``:`s`}</span>
        <button
          type="button"
          class="cfg-array__add"
          ?disabled=${l}
          @click=${()=>{u(a,[...S,c(x)])}}
        >
          <span class="cfg-array__add-icon">${A.plus}</span>
          Add
        </button>
      </div>
      ${v?e`<div class="cfg-array__help">${v}</div>`:n}
      ${S.length===0?e` <div class="cfg-array__empty">No items yet. Click "Add" to create one.</div> `:e`
            <div class="cfg-array__items">
              ${S.map((t,n)=>e`
                  <div class="cfg-array__item">
                    <div class="cfg-array__item-header">
                      <span class="cfg-array__item-index">#${n+1}</span>
                      <button
                        type="button"
                        class="cfg-array__item-remove"
                        title="Remove item"
                        ?disabled=${l}
                        @click=${()=>{let e=[...S];e.splice(n,1),u(a,e)}}
                      >
                        ${A.trash}
                      </button>
                    </div>
                    <div class="cfg-array__item-content">
                      ${H({schema:x,value:t,path:[...a,n],hints:o,rawAvailable:f,unsupported:s,disabled:l,searchCriteria:b,showLabel:!1,revealSensitive:p,isSensitivePathRevealed:m,onToggleSensitivePath:h,onPatch:u})}
                    </div>
                  </div>
                `)}
            </div>
          `}
    </div>
  `}function Y(t){let{schema:n,value:r,path:i,hints:a,rawAvailable:o,unsupported:s,disabled:l,reservedKeys:u,onPatch:d,searchCriteria:f,revealSensitive:p,isSensitivePathRevealed:m,onToggleSensitivePath:g}=t,_=E(n),v=Object.entries(r??{}).filter(([e])=>!u.has(e)),y=f&&P(f)?v.filter(([e,t])=>B({schema:n,value:t,path:[...i,e],hints:a,criteria:f})):v;return e`
    <div class="cfg-map">
      <div class="cfg-map__header">
        <span class="cfg-map__label">Custom entries</span>
        <button
          type="button"
          class="cfg-map__add"
          ?disabled=${l}
          @click=${()=>{let e={...r},t=1,a=`custom-${t}`;for(;a in e;)t+=1,a=`custom-${t}`;e[a]=_?{}:c(n),d(i,e)}}
        >
          <span class="cfg-map__add-icon">${A.plus}</span>
          Add Entry
        </button>
      </div>

      ${y.length===0?e` <div class="cfg-map__empty">No custom entries.</div> `:e`
            <div class="cfg-map__items">
              ${y.map(([t,c])=>{let u=[...i,t],v=D(c),y=M({path:u,value:c,hints:a,revealSensitive:p??!1,isSensitivePathRevealed:m});return e`
                  <div class="cfg-map__item">
                    <div class="cfg-map__item-header">
                      <div class="cfg-map__item-key">
                        <input
                          type="text"
                          class="cfg-input cfg-input--sm"
                          placeholder="Key"
                          .value=${t}
                          ?disabled=${l}
                          @change=${e=>{let n=e.target.value.trim();if(!n||n===t)return;let a={...r};n in a||(a[n]=a[t],delete a[t],d(i,a))}}
                        />
                      </div>
                      <button
                        type="button"
                        class="cfg-map__item-remove"
                        title="Remove entry"
                        ?disabled=${l}
                        @click=${()=>{let e={...r};delete e[t],d(i,e)}}
                      >
                        ${A.trash}
                      </button>
                    </div>
                    <div class="cfg-map__item-value">
                      ${_?e`
                            <div class="cfg-input-wrap">
                              <textarea
                                class="cfg-textarea cfg-textarea--sm${y.isRedacted?` cfg-textarea--redacted`:``}"
                                placeholder=${y.isRedacted?h:`JSON value`}
                                rows="2"
                                .value=${y.isRedacted?``:v}
                                ?disabled=${l}
                                ?readonly=${y.isRedacted}
                                @click=${()=>{y.isRedacted&&g&&g(u)}}
                                @change=${e=>{if(y.isRedacted)return;let t=e.target,n=t.value.trim();if(!n){d(u,void 0);return}try{d(u,JSON.parse(n))}catch{t.value=v}}}
                              ></textarea>
                              ${N({path:u,state:y,disabled:l,onToggleSensitivePath:g})}
                            </div>
                          `:H({schema:n,value:c,path:u,hints:a,rawAvailable:o,unsupported:s,disabled:l,searchCriteria:f,showLabel:!1,revealSensitive:p,isSensitivePathRevealed:m,onToggleSensitivePath:g,onPatch:d})}
                    </div>
                  </div>
                `})}
            </div>
          `}
    </div>
  `}var X={env:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <circle cx="12" cy="12" r="3"></circle>
      <path
        d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"
      ></path>
    </svg>
  `,update:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
      <polyline points="7 10 12 15 17 10"></polyline>
      <line x1="12" y1="15" x2="12" y2="3"></line>
    </svg>
  `,agents:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <path
        d="M12 2a2 2 0 0 1 2 2c0 .74-.4 1.39-1 1.73V7h1a7 7 0 0 1 7 7h1a1 1 0 0 1 1 1v3a1 1 0 0 1-1 1h-1v1a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-1H2a1 1 0 0 1-1-1v-3a1 1 0 0 1 1-1h1a7 7 0 0 1 7-7h1V5.73c-.6-.34-1-.99-1-1.73a2 2 0 0 1 2-2z"
      ></path>
      <circle cx="8" cy="14" r="1"></circle>
      <circle cx="16" cy="14" r="1"></circle>
    </svg>
  `,auth:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <rect x="3" y="11" width="18" height="11" rx="2" ry="2"></rect>
      <path d="M7 11V7a5 5 0 0 1 10 0v4"></path>
    </svg>
  `,channels:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"></path>
    </svg>
  `,messages:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <path d="M4 4h16c1.1 0 2 .9 2 2v12c0 1.1-.9 2-2 2H4c-1.1 0-2-.9-2-2V6c0-1.1.9-2 2-2z"></path>
      <polyline points="22,6 12,13 2,6"></polyline>
    </svg>
  `,commands:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <polyline points="4 17 10 11 4 5"></polyline>
      <line x1="12" y1="19" x2="20" y2="19"></line>
    </svg>
  `,hooks:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"></path>
      <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"></path>
    </svg>
  `,skills:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <polygon
        points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"
      ></polygon>
    </svg>
  `,tools:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <path
        d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"
      ></path>
    </svg>
  `,gateway:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <circle cx="12" cy="12" r="10"></circle>
      <line x1="2" y1="12" x2="22" y2="12"></line>
      <path
        d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"
      ></path>
    </svg>
  `,wizard:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <path d="M15 4V2"></path>
      <path d="M15 16v-2"></path>
      <path d="M8 9h2"></path>
      <path d="M20 9h2"></path>
      <path d="M17.8 11.8 19 13"></path>
      <path d="M15 9h0"></path>
      <path d="M17.8 6.2 19 5"></path>
      <path d="m3 21 9-9"></path>
      <path d="M12.2 6.2 11 5"></path>
    </svg>
  `,meta:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <path d="M12 20h9"></path>
      <path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z"></path>
    </svg>
  `,logging:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path>
      <polyline points="14 2 14 8 20 8"></polyline>
      <line x1="16" y1="13" x2="8" y2="13"></line>
      <line x1="16" y1="17" x2="8" y2="17"></line>
      <polyline points="10 9 9 9 8 9"></polyline>
    </svg>
  `,browser:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <circle cx="12" cy="12" r="10"></circle>
      <circle cx="12" cy="12" r="4"></circle>
      <line x1="21.17" y1="8" x2="12" y2="8"></line>
      <line x1="3.95" y1="6.06" x2="8.54" y2="14"></line>
      <line x1="10.88" y1="21.94" x2="15.46" y2="14"></line>
    </svg>
  `,ui:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
      <line x1="3" y1="9" x2="21" y2="9"></line>
      <line x1="9" y1="21" x2="9" y2="9"></line>
    </svg>
  `,models:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <path
        d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"
      ></path>
      <polyline points="3.27 6.96 12 12.01 20.73 6.96"></polyline>
      <line x1="12" y1="22.08" x2="12" y2="12"></line>
    </svg>
  `,bindings:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <rect x="2" y="2" width="20" height="8" rx="2" ry="2"></rect>
      <rect x="2" y="14" width="20" height="8" rx="2" ry="2"></rect>
      <line x1="6" y1="6" x2="6.01" y2="6"></line>
      <line x1="6" y1="18" x2="6.01" y2="18"></line>
    </svg>
  `,broadcast:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <path d="M4.9 19.1C1 15.2 1 8.8 4.9 4.9"></path>
      <path d="M7.8 16.2c-2.3-2.3-2.3-6.1 0-8.5"></path>
      <circle cx="12" cy="12" r="2"></circle>
      <path d="M16.2 7.8c2.3 2.3 2.3 6.1 0 8.5"></path>
      <path d="M19.1 4.9C23 8.8 23 15.1 19.1 19"></path>
    </svg>
  `,audio:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <path d="M9 18V5l12-2v13"></path>
      <circle cx="6" cy="18" r="3"></circle>
      <circle cx="18" cy="16" r="3"></circle>
    </svg>
  `,session:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"></path>
      <circle cx="9" cy="7" r="4"></circle>
      <path d="M23 21v-2a4 4 0 0 0-3-3.87"></path>
      <path d="M16 3.13a4 4 0 0 1 0 7.75"></path>
    </svg>
  `,cron:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <circle cx="12" cy="12" r="10"></circle>
      <polyline points="12 6 12 12 16 14"></polyline>
    </svg>
  `,web:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <circle cx="12" cy="12" r="10"></circle>
      <line x1="2" y1="12" x2="22" y2="12"></line>
      <path
        d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"
      ></path>
    </svg>
  `,discovery:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <circle cx="11" cy="11" r="8"></circle>
      <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
    </svg>
  `,canvasHost:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
      <circle cx="8.5" cy="8.5" r="1.5"></circle>
      <polyline points="21 15 16 10 5 21"></polyline>
    </svg>
  `,talk:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <path d="M12 1a3 3 0 0 0-3 3v8a3 3 0 0 0 6 0V4a3 3 0 0 0-3-3z"></path>
      <path d="M19 10v2a7 7 0 0 1-14 0v-2"></path>
      <line x1="12" y1="19" x2="12" y2="23"></line>
      <line x1="8" y1="23" x2="16" y2="23"></line>
    </svg>
  `,plugins:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <path d="M12 2v6"></path>
      <path d="m4.93 10.93 4.24 4.24"></path>
      <path d="M2 12h6"></path>
      <path d="m4.93 13.07 4.24-4.24"></path>
      <path d="M12 22v-6"></path>
      <path d="m19.07 13.07-4.24-4.24"></path>
      <path d="M22 12h-6"></path>
      <path d="m19.07 10.93-4.24 4.24"></path>
    </svg>
  `,diagnostics:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <polyline points="22 12 18 12 15 21 9 3 6 12 2 12"></polyline>
    </svg>
  `,cli:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <polyline points="4 17 10 11 4 5"></polyline>
      <line x1="12" y1="19" x2="20" y2="19"></line>
    </svg>
  `,secrets:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <path
        d="m21 2-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0 3 3L22 7l-3-3m-3.5 3.5L19 4"
      ></path>
    </svg>
  `,acp:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"></path>
      <circle cx="9" cy="7" r="4"></circle>
      <path d="M23 21v-2a4 4 0 0 0-3-3.87"></path>
      <path d="M16 3.13a4 4 0 0 1 0 7.75"></path>
    </svg>
  `,mcp:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <rect x="2" y="2" width="20" height="8" rx="2" ry="2"></rect>
      <rect x="2" y="14" width="20" height="8" rx="2" ry="2"></rect>
      <line x1="6" y1="6" x2="6.01" y2="6"></line>
      <line x1="6" y1="18" x2="6.01" y2="18"></line>
    </svg>
  `,default:e`
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path>
      <polyline points="14 2 14 8 20 8"></polyline>
    </svg>
  `},Z={env:{label:`Environment Variables`,description:`Environment variables passed to the gateway process`},update:{label:`Updates`,description:`Auto-update settings and release channel`},agents:{label:`Agents`,description:`Agent configurations, models, and identities`},auth:{label:`Authentication`,description:`API keys and authentication profiles`},channels:{label:`Channels`,description:`Messaging channels (Telegram, Discord, Slack, etc.)`},messages:{label:`Messages`,description:`Message handling and routing settings`},commands:{label:`Commands`,description:`Custom slash commands`},hooks:{label:`Hooks`,description:`Webhooks and event hooks`},skills:{label:`Skills`,description:`Skill packs and capabilities`},tools:{label:`Tools`,description:`Tool configurations (browser, search, etc.)`},gateway:{label:`Gateway`,description:`Gateway server settings (port, auth, binding)`},wizard:{label:`Setup Wizard`,description:`Setup wizard state and history`},meta:{label:`Metadata`,description:`Gateway metadata and version information`},logging:{label:`Logging`,description:`Log levels and output configuration`},browser:{label:`Browser`,description:`Browser automation settings`},ui:{label:`UI`,description:`User interface preferences`},models:{label:`Models`,description:`AI model configurations and providers`},bindings:{label:`Bindings`,description:`Key bindings and shortcuts`},broadcast:{label:`Broadcast`,description:`Broadcast and notification settings`},audio:{label:`Audio`,description:`Audio input/output settings`},session:{label:`Session`,description:`Session management and persistence`},cron:{label:`Cron`,description:`Scheduled tasks and automation`},web:{label:`Web`,description:`Web server and API settings`},discovery:{label:`Discovery`,description:`Service discovery and networking`},canvasHost:{label:`Canvas Host`,description:`Canvas rendering and display`},talk:{label:`Talk`,description:`Voice and speech settings`},plugins:{label:`Plugins`,description:`Plugin management and extensions`},diagnostics:{label:`Diagnostics`,description:`Instrumentation, OpenTelemetry, and cache-trace settings`},cli:{label:`CLI`,description:`CLI banner and startup behavior`},secrets:{label:`Secrets`,description:`Secret provider configuration`},acp:{label:`ACP`,description:`Agent Communication Protocol runtime and streaming settings`},mcp:{label:`MCP`,description:`Model Context Protocol server definitions`}};function te(e){return X[e]??X.default}function ne(e){if(!e.query)return!0;let t=F(e.query),n=t.text,r=Z[e.key];return n&&(a(e.key).includes(n)||r?.label&&a(r.label).includes(n)||r?.description&&a(r.description).includes(n))&&t.tags.length===0?!0:B({schema:e.schema,value:e.sectionValue,path:[e.key],hints:e.uiHints,criteria:t})}function re(t){if(!t.schema)return e` <div class="muted">配置架构不可用。</div> `;let r=t.schema,i=t.value??{};if(s(r)!==`object`||!r.properties)return e` <div class="callout danger">不支持的配置架构。请使用原始模式。</div> `;let a=new Set(t.unsupportedPaths??[]),c=r.properties,l=t.searchQuery??``,f=F(l),p=t.activeSection,m=t.activeSubsection??null,h=Object.entries(c).toSorted((e,n)=>{let r=u([e[0]],t.uiHints)?.order??50,i=u([n[0]],t.uiHints)?.order??50;return r===i?e[0].localeCompare(n[0]):r-i}).filter(([e,n])=>!(p&&e!==p||l&&!ne({key:e,schema:n,sectionValue:i[e],uiHints:t.uiHints,query:l}))),g=null;if(p&&m&&h.length===1){let e=h[0]?.[1];e&&s(e)===`object`&&e.properties&&e.properties[m]&&(g={sectionKey:p,subsectionKey:m,schema:e.properties[m]})}if(h.length===0)return e`
      <div class="config-empty">
        <div class="config-empty__icon">${o.search}</div>
        <div class="config-empty__text">
          ${l?`No settings match "${l}"`:`No settings in this section`}
        </div>
      </div>
    `;let _=r=>e`
    <section class="config-section-card" id=${r.id}>
      <div class="config-section-card__header">
        <span class="config-section-card__icon">${te(r.sectionKey)}</span>
        <div class="config-section-card__titles">
          <h3 class="config-section-card__title">${r.label}</h3>
          ${r.description?e`<p class="config-section-card__desc">${r.description}</p>`:n}
        </div>
      </div>
      <div class="config-section-card__content">
        ${H({schema:r.node,value:r.nodeValue,path:r.path,hints:t.uiHints,rawAvailable:t.rawAvailable??!0,unsupported:a,disabled:t.disabled??!1,showLabel:!1,searchCriteria:f,revealSensitive:t.revealSensitive??!1,isSensitivePathRevealed:t.isSensitivePathRevealed,onToggleSensitivePath:t.onToggleSensitivePath,onPatch:t.onPatch})}
      </div>
    </section>
  `;return e`
    <div class="config-form config-form--modern">
      ${g?(()=>{let{sectionKey:e,subsectionKey:n,schema:r}=g,a=u([e,n],t.uiHints),o=a?.label??r.title??d(n),s=a?.help??r.description??``,c=i[e],l=c&&typeof c==`object`?c[n]:void 0;return _({id:`config-section-${e}-${n}`,sectionKey:e,label:o,description:s,node:r,nodeValue:l,path:[e,n]})})():h.map(([e,t])=>{let n=Z[e]??{label:e.charAt(0).toUpperCase()+e.slice(1),description:t.description??``};return _({id:`config-section-${e}`,sectionKey:e,label:n.label,description:n.description,node:t,nodeValue:i[e],path:[e]})})}
    </div>
  `}var ie=new Set([`title`,`description`,`default`,`nullable`]);function ae(e){return Object.keys(e??{}).filter(e=>!ie.has(e)).length===0}function Q(e){let t=e.filter(e=>e!=null),n=t.length!==e.length,r=[];for(let e of t)r.some(t=>Object.is(t,e))||r.push(e);return{enumValues:r,nullable:n}}function oe(e){return!e||typeof e!=`object`?{schema:null,unsupportedPaths:[`<root>`]}:$(e,[])}function $(e,t){let n=new Set,r={...e},i=l(t)||`<root>`;if(e.anyOf||e.oneOf||e.allOf)return ue(e,t)||{schema:e,unsupportedPaths:[i]};let a=Array.isArray(e.type)&&e.type.includes(`null`),o=s(e)??(e.properties||e.additionalProperties?`object`:void 0);if(r.type=o??e.type,r.nullable=a||e.nullable,r.enum){let{enumValues:e,nullable:t}=Q(r.enum);r.enum=e,t&&(r.nullable=!0),e.length===0&&n.add(i)}if(o===`object`){let a=e.properties??{},o={};for(let[e,r]of Object.entries(a)){let i=$(r,[...t,e]);i.schema&&(o[e]=i.schema);for(let e of i.unsupportedPaths)n.add(e)}if(r.properties=o,e.additionalProperties===!0)r.additionalProperties={};else if(e.additionalProperties===!1)r.additionalProperties=!1;else if(e.additionalProperties&&typeof e.additionalProperties==`object`&&!ae(e.additionalProperties)){let a=$(e.additionalProperties,[...t,`*`]);r.additionalProperties=a.schema??e.additionalProperties,a.unsupportedPaths.length>0&&n.add(i)}}else if(o===`array`){let a=Array.isArray(e.items)?e.items[0]:e.items;if(!a)n.add(i);else{let e=$(a,[...t,`*`]);r.items=e.schema??a,e.unsupportedPaths.length>0&&n.add(i)}}else o!==`string`&&o!==`number`&&o!==`integer`&&o!==`boolean`&&!r.enum&&n.add(i);return{schema:r,unsupportedPaths:Array.from(n)}}function se(e){if(s(e)!==`object`)return!1;let t=e.properties?.source,n=e.properties?.provider,r=e.properties?.id;return!t||!n||!r?!1:typeof t.const==`string`&&s(n)===`string`&&s(r)===`string`}function ce(e){let t=e.oneOf??e.anyOf;return!t||t.length===0?!1:t.every(e=>se(e))}function le(e,t,n,r){let i=n.findIndex(e=>s(e)===`string`);if(i<0)return null;let a=n.filter((e,t)=>t!==i);return a.length!==1||!ce(a[0])?null:$({...e,...n[i],nullable:r,anyOf:void 0,oneOf:void 0,allOf:void 0},t)}function ue(e,t){if(e.allOf)return null;let n=e.anyOf??e.oneOf;if(!n)return null;let r=[],i=[],a=!1;for(let e of n){if(!e||typeof e!=`object`)return null;if(Array.isArray(e.enum)){let{enumValues:t,nullable:n}=Q(e.enum);r.push(...t),n&&(a=!0);continue}if(`const`in e){if(e.const==null){a=!0;continue}r.push(e.const);continue}if(s(e)===`null`){a=!0;continue}i.push(e)}let o=le(e,t,i,a);if(o)return o;if(r.length>0&&i.length===0){let t=[];for(let e of r)t.some(t=>Object.is(t,e))||t.push(e);return{schema:{...e,enum:t,nullable:a,anyOf:void 0,oneOf:void 0,allOf:void 0},unsupportedPaths:[]}}if(i.length===1){let e=$(i[0],t);return e.schema&&(e.schema.nullable=a||e.schema.nullable),e}let c=new Set([`string`,`number`,`integer`,`boolean`,`object`,`array`]);return i.length>0&&r.length===0&&i.every(e=>{let t=s(e);return!!t&&c.has(String(t))})?{schema:{...e,nullable:a},unsupportedPaths:[]}:null}export{w as a,x as c,l as d,s as f,H as i,d as l,Z as n,C as o,re as r,h as s,oe as t,_ as u};
//# sourceMappingURL=config-form-x_UhxUYO.js.map