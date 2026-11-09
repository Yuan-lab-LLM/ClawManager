import { useEffect, useMemo, useRef, useState, type ComponentProps } from 'react';
import { ArrowLeft } from 'lucide-react';
import { Link, useLocation, useParams } from 'react-router-dom';
import type UserLayout from '../../components/UserLayout';
import { TeamPageEnvironmentProvider } from '../../components/teams/TeamPageEnvironment';
import { ownerTeamWorkspaceAPI } from '../../services/ownerTeamService';
import { ieiSystemService } from '../../services/ieiSystemService';
import { ownerTeamOverviewURL } from '../../lib/ownerPortalNavigation';
import TeamDetailPage from './TeamDetailPage';
import '../../components/teams/owner-team-workspace.css';

function NorthboundTeamLayout({ title, titleAccessory, children }: ComponentProps<typeof UserLayout>) {
  const { id } = useParams(); const location = useLocation();
  return <main className="ot-northbound-layout"><header className="ot-northbound-header"><div><Link className="ot-secondary ot-return" to={ownerTeamOverviewURL(Number(id), location.state?.portalReturnSearch || '')}><ArrowLeft size={15}/>我的团队空间</Link><h1>{title || '团队对话'}</h1></div>{titleAccessory}</header><section className="ot-northbound-body">{children}</section></main>;
}

export default function IEISystemTeamPage() {
  const { id } = useParams(); const location = useLocation();
  const [owner, setOwner] = useState(''); const [error, setError] = useState('');
  const priorSearch = typeof location.state?.portalReturnSearch === 'string' ? location.state.portalReturnSearch : '';
  const initialSearch = useRef(priorSearch); initialSearch.current = priorSearch;
  useEffect(() => {
    let active = true; setOwner(''); setError('');
    void ieiSystemService.getSession().then(session => { if (active) setOwner(session.owner); }).catch(error => { if (active) setError(error instanceof Error ? error.message : '访问会话无效'); });
    return () => { active = false; };
  }, [id]);
  const environment = useMemo(() => ({ api: ownerTeamWorkspaceAPI, owner, Layout: NorthboundTeamLayout, returnURL: ownerTeamOverviewURL(Number(id), initialSearch.current), memberEntry: (instanceID: number) => ({ to: `/ieisystem/instances/${instanceID}`, state: { portalReturnSearch: initialSearch.current, portalReturnTeamID: Number(id) } }) }), [owner, id]);
  if (error) return <main className="ot-workspace"><h1>访问验证失败</h1><p role="alert">{error}</p><Link to="/ieisystem/list-instances">返回门户</Link></main>;
  if (!owner) return <main className="ot-workspace" role="status">正在验证团队访问权限…</main>;
  return <TeamPageEnvironmentProvider value={environment}><TeamDetailPage key={`${owner}:${id}`}/></TeamPageEnvironmentProvider>;
}
