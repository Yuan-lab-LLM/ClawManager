// Development-only acceptance entry. Not imported by the application bundle.
import React from 'react';
import { createRoot } from 'react-dom/client';
import { OwnerTeamTemplates } from '../../src/components/teams/OwnerTeamTemplates';
import { OwnerTeamDashboard } from '../../src/components/teams/OwnerTeamWorkspace';
import { MemoryRouter } from 'react-router-dom';
import type { OwnerTeamsState } from '../../src/hooks/useOwnerTeams';
import '../../src/index.css';
import '../../src/components/teams/owner-team-workspace.css';

const capabilities = {can_create:true,enabled:true,worker_runtime_types:['openclaw','hermes','opencode','deepseek-harness']};
const state = {owner:'fixture@example.com',capabilities,teams:[{id:99,name:'论文精读验收团队',member_count:6,available_count:6,members:[],status:'running'}],selectedID:99,query:'',setQuery:()=>{},select:()=>{},refresh:async()=>{},loadMore:()=>{},hasMore:false,loading:false} as unknown as OwnerTeamsState;
const dashboard = new URLSearchParams(location.search).has('dashboard');
createRoot(document.getElementById('root')!).render(dashboard ? <MemoryRouter><div style={{height:'calc(100dvh - 24px)',margin:12}}><OwnerTeamDashboard state={state}/></div></MemoryRouter> : <main className="ot-workspace" style={{ height: 'calc(100dvh - 24px)', margin: 12 }}><OwnerTeamTemplates owner="fixture@example.com" canGenerate onOperation={() => {}}/></main>);
