import { useMemo, type ComponentProps } from 'react';
import { useLocation, useOutletContext, useParams } from 'react-router-dom';
import type UserLayout from '../../components/UserLayout';
import { TeamPageEnvironmentProvider } from '../../components/teams/TeamPageEnvironment';
import { ownerTeamWorkspaceAPI } from '../../services/ownerTeamService';
import { ownerInstanceURL, ownerTeamOverviewURL, type OwnerWorkspaceOutlet } from '../../lib/ownerPortalNavigation';
import TeamDetailPage from './TeamDetailPage';
import '../../components/teams/owner-team-workspace.css';

function NorthboundTeamLayout({ children }: ComponentProps<typeof UserLayout>) {
  return <main className="ot-northbound-layout portal-unified-team" aria-label="团队对话">
    <section className="ot-northbound-body">{children}</section>
  </main>;
}

export default function IEISystemTeamPage() {
  const { id } = useParams(); const location = useLocation();
  const { owner } = useOutletContext<OwnerWorkspaceOutlet>();
  const environment = useMemo(() => ({
    api: ownerTeamWorkspaceAPI,
    owner,
    Layout: NorthboundTeamLayout,
    returnURL: ownerTeamOverviewURL(Number(id), location.search),
    memberEntry: (instanceID: number) => ({
      to: ownerInstanceURL(instanceID, location.search, Number(id)),
      state: { portalReturnSearch: location.search, portalReturnTeamID: Number(id), portalReturnFromChat: true },
    }),
  }), [owner, id, location.search]);
  return <TeamPageEnvironmentProvider value={environment}><TeamDetailPage key={`${owner}:${id}`}/></TeamPageEnvironmentProvider>;
}
