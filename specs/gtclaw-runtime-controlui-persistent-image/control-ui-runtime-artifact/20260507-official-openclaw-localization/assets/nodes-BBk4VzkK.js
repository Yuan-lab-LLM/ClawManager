import{f as e,r as t,u as n}from"./i18n-B06L7jQN.js";import{r,t as i,u as a}from"./format-Dg6tFpW6.js";import{r as o}from"./string-coerce-BcFtIWA_.js";function s(e){let t=e?.agents??{},n=Array.isArray(t.list)?t.list:[],r=[];return n.forEach((e,t)=>{if(!e||typeof e!=`object`)return;let n=e,i=o(n.id)??``;if(!i)return;let a=o(n.name),s=n.default===!0;r.push({id:i,name:a,isDefault:s,index:t,record:n})}),r}function c(e,t){let n=new Set(t),r=[];for(let t of e){if(!(Array.isArray(t.commands)?t.commands:[]).some(e=>n.has(String(e))))continue;let e=o(t.nodeId)??``;if(!e)continue;let i=o(t.displayName)??e;r.push({id:e,label:i===e?e:`${i} · ${e}`})}return r.sort((e,t)=>e.label.localeCompare(t.label)),r}var l=`__defaults__`,u=[{value:`deny`,label:`拒绝`},{value:`allowlist`,label:`允许列表`},{value:`full`,label:`完全允许`}],d=[{value:`off`,label:`关闭`},{value:`on-miss`,label:`未命中时`},{value:`always`,label:`始终`}];function f(e){return e===`allowlist`||e===`full`||e===`deny`?e:`deny`}function p(e){return e===`always`||e===`off`||e===`on-miss`?e:`on-miss`}function m(e){let t=e?.defaults??{};return{security:f(t.security),ask:p(t.ask),askFallback:f(t.askFallback??`deny`),autoAllowSkills:t.autoAllowSkills??!1}}function h(e){return s(e).map(e=>({id:e.id,name:e.name,isDefault:e.isDefault}))}function g(e,t){let n=h(e),r=Object.keys(t?.agents??{}),i=new Map;n.forEach(e=>i.set(e.id,e)),r.forEach(e=>{i.has(e)||i.set(e,{id:e})});let a=Array.from(i.values());return a.length===0&&a.push({id:`main`,isDefault:!0}),a.sort((e,t)=>{if(e.isDefault&&!t.isDefault)return-1;if(!e.isDefault&&t.isDefault)return 1;let n=e.name?.trim()?e.name:e.id,r=t.name?.trim()?t.name:t.id;return n.localeCompare(r)}),a}function _(e,t){return e===l?l:e&&t.some(t=>t.id===e)?e:l}function v(e){let t=e.execApprovalsForm??e.execApprovalsSnapshot?.file??null,n=!!t,r=m(t),i=g(e.configForm,t),a=T(e.nodes),o=e.execApprovalsTarget,s=o===`node`&&e.execApprovalsTargetNodeId?e.execApprovalsTargetNodeId:null;o===`node`&&s&&!a.some(e=>e.id===s)&&(s=null);let c=_(e.execApprovalsSelectedAgent,i),u=c===l?null:(t?.agents??{})[c]??null,d=Array.isArray(u?.allowlist)?u.allowlist??[]:[];return{ready:n,disabled:e.execApprovalsSaving||e.execApprovalsLoading,dirty:e.execApprovalsDirty,loading:e.execApprovalsLoading,saving:e.execApprovalsSaving,form:t,defaults:r,selectedScope:c,selectedAgent:u,agents:i,allowlist:d,target:o,targetNodeId:s,targetNodes:a,onSelectScope:e.onExecApprovalsSelectAgent,onSelectTarget:e.onExecApprovalsTargetChange,onPatch:e.onExecApprovalsPatch,onRemove:e.onExecApprovalsRemove,onLoad:e.onLoadExecApprovals,onSave:e.onSaveExecApprovals}}function y(r){let i=r.ready,a=r.target!==`node`||!!r.targetNodeId;return e`
    <section class="card">
      <div class="row" style="justify-content: space-between; align-items: center;">
        <div>
          <div class="card-title">Exec 审批</div>
          <div class="card-sub">
            <span class="mono">exec host=gateway/node</span> 的允许列表和审批策略。
          </div>
        </div>
        <button
          class="btn"
          ?disabled=${r.disabled||!r.dirty||!a}
          @click=${r.onSave}
        >
          ${r.saving?`保存中…`:`保存`}
        </button>
      </div>

      ${b(r)}
      ${i?e`
            ${x(r)} ${S(r)}
            ${r.selectedScope===l?n:C(r)}
          `:e`<div class="row" style="margin-top: 12px; gap: 12px;">
            <div class="muted">加载 Exec 审批以编辑允许列表。</div>
            <button class="btn" ?disabled=${r.loading||!a} @click=${r.onLoad}>
              ${r.loading?t(`common.loading`):t(`common.loadApprovals`)}
            </button>
          </div>`}
    </section>
  `}function b(t){let r=t.targetNodes.length>0,i=t.targetNodeId??``;return e`
    <div class="list" style="margin-top: 12px;">
      <div class="list-item">
        <div class="list-main">
          <div class="list-title">目标</div>
          <div class="list-sub">网关编辑本地审批；节点编辑所选节点。</div>
        </div>
        <div class="list-meta">
          <label class="field">
            <span>主机</span>
            <select
              ?disabled=${t.disabled}
              @change=${e=>{if(e.target.value===`node`){let e=t.targetNodes[0]?.id??null;t.onSelectTarget(`node`,i||e)}else t.onSelectTarget(`gateway`,null)}}
            >
              <option value="gateway" ?selected=${t.target===`gateway`}>网关</option>
              <option value="node" ?selected=${t.target===`node`}>节点</option>
            </select>
          </label>
          ${t.target===`node`?e`
                <label class="field">
                  <span>节点</span>
                  <select
                    ?disabled=${t.disabled||!r}
                    @change=${e=>{let n=e.target.value.trim();t.onSelectTarget(`node`,n||null)}}
                  >
                    <option value="" ?selected=${i===``}>选择节点</option>
                    ${t.targetNodes.map(t=>e`<option value=${t.id} ?selected=${i===t.id}>
                          ${t.label}
                        </option>`)}
                  </select>
                </label>
              `:n}
        </div>
      </div>
      ${t.target===`node`&&!r?e` <div class="muted">尚无节点提供 exec 审批。</div> `:n}
    </div>
  `}function x(t){return e`
    <div class="row" style="margin-top: 12px; gap: 8px; flex-wrap: wrap;">
      <span class="label">范围</span>
      <div class="row" style="gap: 8px; flex-wrap: wrap;">
        <button
          class="btn btn--sm ${t.selectedScope===l?`active`:``}"
          @click=${()=>t.onSelectScope(l)}
        >
          默认值
        </button>
        ${t.agents.map(n=>{let r=n.name?.trim()?`${n.name} (${n.id})`:n.id;return e`
            <button
              class="btn btn--sm ${t.selectedScope===n.id?`active`:``}"
              @click=${()=>t.onSelectScope(n.id)}
            >
              ${r}
            </button>
          `})}
      </div>
    </div>
  `}function S(t){let r=t.selectedScope===l,i=t.defaults,a=t.selectedAgent??{},o=r?[`defaults`]:[`agents`,t.selectedScope],s=typeof a.security==`string`?a.security:void 0,c=typeof a.ask==`string`?a.ask:void 0,f=typeof a.askFallback==`string`?a.askFallback:void 0,p=r?i.security:s??`__default__`,m=r?i.ask:c??`__default__`,h=r?i.askFallback:f??`__default__`,g=typeof a.autoAllowSkills==`boolean`?a.autoAllowSkills:void 0,_=g??i.autoAllowSkills,v=g==null;return e`
    <div class="list" style="margin-top: 16px;">
      <div class="list-item">
        <div class="list-main">
          <div class="list-title">安全</div>
          <div class="list-sub">
            ${r?`默认安全模式。`:`默认：${i.security}。`}
          </div>
        </div>
        <div class="list-meta">
          <label class="field">
            <span>模式</span>
            <select
              ?disabled=${t.disabled}
              @change=${e=>{let n=e.target.value;!r&&n===`__default__`?t.onRemove([...o,`security`]):t.onPatch([...o,`security`],n)}}
            >
              ${r?n:e`<option value="__default__" ?selected=${p===`__default__`}>
                    使用默认值（${i.security}）
                  </option>`}
              ${u.map(t=>e`<option value=${t.value} ?selected=${p===t.value}>
                    ${t.label}
                  </option>`)}
            </select>
          </label>
        </div>
      </div>

      <div class="list-item">
        <div class="list-main">
          <div class="list-title">询问</div>
          <div class="list-sub">
            ${r?`默认提示策略。`:`默认：${i.ask}。`}
          </div>
        </div>
        <div class="list-meta">
          <label class="field">
            <span>模式</span>
            <select
              ?disabled=${t.disabled}
              @change=${e=>{let n=e.target.value;!r&&n===`__default__`?t.onRemove([...o,`ask`]):t.onPatch([...o,`ask`],n)}}
            >
              ${r?n:e`<option value="__default__" ?selected=${m===`__default__`}>
                    使用默认值（${i.ask}）
                  </option>`}
              ${d.map(t=>e`<option value=${t.value} ?selected=${m===t.value}>
                    ${t.label}
                  </option>`)}
            </select>
          </label>
        </div>
      </div>

      <div class="list-item">
        <div class="list-main">
          <div class="list-title">询问回退</div>
          <div class="list-sub">
            ${r?`当 UI 提示不可用时应用。`:`默认：${i.askFallback}。`}
          </div>
        </div>
        <div class="list-meta">
          <label class="field">
            <span>回退</span>
            <select
              ?disabled=${t.disabled}
              @change=${e=>{let n=e.target.value;!r&&n===`__default__`?t.onRemove([...o,`askFallback`]):t.onPatch([...o,`askFallback`],n)}}
            >
              ${r?n:e`<option value="__default__" ?selected=${h===`__default__`}>
                    使用默认值（${i.askFallback}）
                  </option>`}
              ${u.map(t=>e`<option value=${t.value} ?selected=${h===t.value}>
                    ${t.label}
                  </option>`)}
            </select>
          </label>
        </div>
      </div>

      <div class="list-item">
        <div class="list-main">
          <div class="list-title">自动允许技能 CLI</div>
          <div class="list-sub">
            ${r?`允许网关列出的技能可执行文件。`:v?`使用默认值（${i.autoAllowSkills?`开启`:`关闭`}）。`:`覆盖（${_?`开启`:`关闭`}）。`}
          </div>
        </div>
        <div class="list-meta">
          <label class="field">
            <span>启用</span>
            <input
              type="checkbox"
              ?disabled=${t.disabled}
              .checked=${_}
              @change=${e=>{let n=e.target;t.onPatch([...o,`autoAllowSkills`],n.checked)}}
            />
          </label>
          ${!r&&!v?e`<button
                class="btn btn--sm"
                ?disabled=${t.disabled}
                @click=${()=>t.onRemove([...o,`autoAllowSkills`])}
              >
                使用默认值
              </button>`:n}
        </div>
      </div>
    </div>
  `}function C(t){let n=[`agents`,t.selectedScope,`allowlist`],r=t.allowlist;return e`
    <div class="row" style="margin-top: 18px; justify-content: space-between;">
      <div>
        <div class="card-title">允许列表</div>
        <div class="card-sub">不区分大小写的 glob 模式。</div>
      </div>
      <button
        class="btn btn--sm"
        ?disabled=${t.disabled}
        @click=${()=>{let e=[...r,{pattern:``}];t.onPatch(n,e)}}
      >
        添加模式
      </button>
    </div>
    <div class="list" style="margin-top: 12px;">
      ${r.length===0?e` <div class="muted">尚无允许列表条目。</div> `:r.map((e,n)=>w(t,e,n))}
    </div>
  `}function w(t,r,o){let s=r.lastUsedAt?a(r.lastUsedAt):`never`,c=r.lastUsedCommand?i(r.lastUsedCommand,120):null,l=r.lastResolvedPath?i(r.lastResolvedPath,120):null;return e`
    <div class="list-item">
      <div class="list-main">
        <div class="list-title">${r.pattern?.trim()?r.pattern:`新模式`}</div>
        <div class="list-sub">上次使用：${s}</div>
        ${c?e`<div class="list-sub mono">${c}</div>`:n}
        ${l?e`<div class="list-sub mono">${l}</div>`:n}
      </div>
      <div class="list-meta">
        <label class="field">
          <span>模式</span>
          <input
            type="text"
            .value=${r.pattern??``}
            ?disabled=${t.disabled}
            @input=${e=>{let n=e.target;t.onPatch([`agents`,t.selectedScope,`allowlist`,o,`pattern`],n.value)}}
          />
        </label>
        <button
          class="btn btn--sm danger"
          ?disabled=${t.disabled}
          @click=${()=>{if(t.allowlist.length<=1){t.onRemove([`agents`,t.selectedScope,`allowlist`]);return}t.onRemove([`agents`,t.selectedScope,`allowlist`,o])}}
        >
          移除
        </button>
      </div>
    </div>
  `}function T(e){return c(e,[`system.execApprovals.get`,`system.execApprovals.set`])}function E(n){let r=j(n);return e`
    ${y(v(n))} ${M(r)} ${D(n)}
    <section class="card">
      <div class="row" style="justify-content: space-between;">
        <div>
          <div class="card-title">节点</div>
          <div class="card-sub">已配对设备和实时链接。</div>
        </div>
        <button class="btn" ?disabled=${n.loading} @click=${n.onRefresh}>
          ${n.loading?t(`common.loading`):t(`common.refresh`)}
        </button>
      </div>
      <div class="list" style="margin-top: 16px;">
        ${n.nodes.length===0?e` <div class="muted">没有找到节点。</div> `:n.nodes.map(e=>I(e))}
      </div>
    </section>
  `}function D(r){let i=r.devicesList??{pending:[],paired:[]},a=Array.isArray(i.pending)?i.pending:[],o=Array.isArray(i.paired)?i.paired:[];return e`
    <section class="card">
      <div class="row" style="justify-content: space-between;">
        <div>
          <div class="card-title">设备</div>
          <div class="card-sub">配对请求和角色凭证。</div>
        </div>
        <button class="btn" ?disabled=${r.devicesLoading} @click=${r.onDevicesRefresh}>
          ${r.devicesLoading?t(`common.loading`):t(`common.refresh`)}
        </button>
      </div>
      ${r.devicesError?e`<div class="callout danger" style="margin-top: 12px;">${r.devicesError}</div>`:n}
      <div class="list" style="margin-top: 16px;">
        ${a.length>0?e`
              <div class="muted" style="margin-bottom: 8px;">待处理</div>
              ${a.map(e=>O(e,r))}
            `:n}
        ${o.length>0?e`
              <div class="muted" style="margin-top: 12px; margin-bottom: 8px;">已配对</div>
              ${o.map(e=>k(e,r))}
            `:n}
        ${a.length===0&&o.length===0?e` <div class="muted">没有已配对设备。</div> `:n}
      </div>
    </section>
  `}function O(n,i){let s=o(n.displayName)||n.deviceId,c=typeof n.ts==`number`?a(n.ts):t(`common.na`),l=o(n.role)||r(n.roles),u=r(n.scopes),d=n.isRepair?` · repair`:``,f=n.remoteIp?` · ${n.remoteIp}`:``;return e`
    <div class="list-item">
      <div class="list-main">
        <div class="list-title">${s}</div>
        <div class="list-sub">${n.deviceId}${f}</div>
        <div class="muted" style="margin-top: 6px;">
          角色：${l} · 范围：${u} · 请求于 ${c}${d}
        </div>
      </div>
      <div class="list-meta">
        <div class="row" style="justify-content: flex-end; gap: 8px; flex-wrap: wrap;">
          <button class="btn btn--sm primary" @click=${()=>i.onDeviceApprove(n.requestId)}>
            批准
          </button>
          <button class="btn btn--sm" @click=${()=>i.onDeviceReject(n.requestId)}>
            拒绝
          </button>
        </div>
      </div>
    </div>
  `}function k(t,n){let i=o(t.displayName)||t.deviceId,a=t.remoteIp?` · ${t.remoteIp}`:``,s=`roles: ${r(t.roles)}`,c=`scopes: ${r(t.scopes)}`,l=Array.isArray(t.tokens)?t.tokens:[];return e`
    <div class="list-item">
      <div class="list-main">
        <div class="list-title">${i}</div>
        <div class="list-sub">${t.deviceId}${a}</div>
        <div class="muted" style="margin-top: 6px;">${s} · ${c}</div>
        ${l.length===0?e` <div class="muted" style="margin-top: 6px">凭证：无</div> `:e`
              <div class="muted" style="margin-top: 10px;">凭证</div>
              <div style="display: flex; flex-direction: column; gap: 8px; margin-top: 6px;">
                ${l.map(e=>A(t.deviceId,e,n))}
              </div>
            `}
      </div>
    </div>
  `}function A(t,i,o){let s=i.revokedAtMs?`已撤销`:`活跃`,c=`范围：${r(i.scopes)}`,l=a(i.rotatedAtMs??i.createdAtMs??i.lastUsedAtMs??null);return e`
    <div class="row" style="justify-content: space-between; gap: 8px;">
      <div class="list-sub">${i.role} · ${s} · ${c} · ${l}</div>
      <div class="row" style="justify-content: flex-end; gap: 6px; flex-wrap: wrap;">
        <button
          class="btn btn--sm"
          @click=${()=>o.onDeviceRotate(t,i.role,i.scopes)}
        >
          轮换
        </button>
        ${i.revokedAtMs?n:e`
              <button
                class="btn btn--sm danger"
                @click=${()=>o.onDeviceRevoke(t,i.role)}
              >
                撤销
              </button>
            `}
      </div>
    </div>
  `}function j(e){let t=e.configForm,n=P(e.nodes),{defaultBinding:r,agents:i}=F(t);return{ready:!!t,disabled:e.configSaving||e.configFormMode===`raw`,configDirty:e.configDirty,configLoading:e.configLoading,configSaving:e.configSaving,defaultBinding:r,agents:i,nodes:n,onBindDefault:e.onBindDefault,onBindAgent:e.onBindAgent,onSave:e.onSaveBindings,onLoadConfig:e.onLoadConfig,formMode:e.configFormMode}}function M(r){let i=r.nodes.length>0,a=r.defaultBinding??``;return e`
    <section class="card">
      <div class="row" style="justify-content: space-between; align-items: center;">
        <div>
          <div class="card-title">${t(`nodes.binding.execNodeBinding`)}</div>
          <div class="card-sub">${t(`nodes.binding.execNodeBindingSubtitle`)}</div>
        </div>
        <button
          class="btn"
          ?disabled=${r.disabled||!r.configDirty}
          @click=${r.onSave}
        >
          ${r.configSaving?t(`common.saving`):t(`common.save`)}
        </button>
      </div>

      ${r.formMode===`raw`?e`
            <div class="callout warn" style="margin-top: 12px">
              ${t(`nodes.binding.formModeHint`)}
            </div>
          `:n}
      ${r.ready?e`
            <div class="list" style="margin-top: 16px;">
              <div class="list-item">
                <div class="list-main">
                  <div class="list-title">${t(`nodes.binding.defaultBinding`)}</div>
                  <div class="list-sub">${t(`nodes.binding.defaultBindingHint`)}</div>
                </div>
                <div class="list-meta">
                  <label class="field">
                    <span>${t(`nodes.binding.node`)}</span>
                    <select
                      ?disabled=${r.disabled||!i}
                      @change=${e=>{let t=e.target.value.trim();r.onBindDefault(t||null)}}
                    >
                      <option value="" ?selected=${a===``}>任意节点</option>
                      ${r.nodes.map(t=>e`<option value=${t.id} ?selected=${a===t.id}>
                            ${t.label}
                          </option>`)}
                    </select>
                  </label>
                  ${i?n:e` <div class="muted">没有可用的 system.run 节点。</div> `}
                </div>
              </div>

              ${r.agents.length===0?e` <div class="muted">未找到代理。</div> `:r.agents.map(e=>N(e,r))}
            </div>
          `:e`<div class="row" style="margin-top: 12px; gap: 12px;">
            <div class="muted">${t(`nodes.binding.loadConfigHint`)}</div>
            <button class="btn" ?disabled=${r.configLoading} @click=${r.onLoadConfig}>
              ${r.configLoading?t(`common.loading`):t(`common.loadConfig`)}
            </button>
          </div>`}
    </section>
  `}function N(t,n){let r=t.binding??`__default__`,i=t.name?.trim()?`${t.name} (${t.id})`:t.id,a=n.nodes.length>0;return e`
    <div class="list-item">
      <div class="list-main">
        <div class="list-title">${i}</div>
        <div class="list-sub">
          ${t.isDefault?`默认代理`:`代理`} ·
          ${r===`__default__`?`使用默认值（${n.defaultBinding??`任意`}）`:`覆盖：${t.binding}`}
        </div>
      </div>
      <div class="list-meta">
        <label class="field">
          <span>绑定</span>
          <select
            ?disabled=${n.disabled||!a}
            @change=${e=>{let r=e.target.value.trim();n.onBindAgent(t.index,r===`__default__`?null:r)}}
          >
            <option value="__default__" ?selected=${r===`__default__`}>
              使用默认值
            </option>
            ${n.nodes.map(t=>e`<option value=${t.id} ?selected=${r===t.id}>
                  ${t.label}
                </option>`)}
          </select>
        </label>
      </div>
    </div>
  `}function P(e){return c(e,[`system.run`])}function F(e){let t={id:`main`,name:void 0,index:0,isDefault:!0,binding:null};if(!e||typeof e!=`object`)return{defaultBinding:null,agents:[t]};let n=(e.tools??{}).exec??{},r=typeof n.node==`string`&&n.node.trim()?n.node.trim():null,i=e.agents??{};if(!Array.isArray(i.list)||i.list.length===0)return{defaultBinding:r,agents:[t]};let a=s(e).map(e=>{let t=(e.record.tools??{}).exec??{},n=typeof t.node==`string`&&t.node.trim()?t.node.trim():null;return{id:e.id,name:e.name,index:e.index,isDefault:e.isDefault,binding:n}});return a.length===0&&a.push(t),{defaultBinding:r,agents:a}}function I(t){let n=!!t.connected,r=!!t.paired,i=typeof t.displayName==`string`&&t.displayName.trim()||(typeof t.nodeId==`string`?t.nodeId:`unknown`),a=Array.isArray(t.caps)?t.caps:[],o=Array.isArray(t.commands)?t.commands:[];return e`
    <div class="list-item">
      <div class="list-main">
        <div class="list-title">${i}</div>
        <div class="list-sub">
          ${typeof t.nodeId==`string`?t.nodeId:``}
          ${typeof t.remoteIp==`string`?` · ${t.remoteIp}`:``}
          ${typeof t.version==`string`?` · ${t.version}`:``}
        </div>
        <div class="chip-row" style="margin-top: 6px;">
          <span class="chip">${r?`已配对`:`未配对`}</span>
          <span class="chip ${n?`chip-ok`:`chip-warn`}">
            ${n?`已连接`:`离线`}
          </span>
          ${a.slice(0,12).map(t=>e`<span class="chip">${String(t)}</span>`)}
          ${o.slice(0,8).map(t=>e`<span class="chip">${String(t)}</span>`)}
        </div>
      </div>
    </div>
  `}export{E as renderNodes};
//# sourceMappingURL=nodes-BBk4VzkK.js.map
