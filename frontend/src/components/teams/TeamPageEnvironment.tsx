import { createContext, useContext, type ComponentType, type ComponentProps, type ReactNode } from 'react';
import UserLayout from '../UserLayout';
import { teamService } from '../../services/teamService';
import type { TeamWorkspaceAPI } from '../../services/ownerTeamService';

export type TeamPageEnvironment = {
  api: TeamWorkspaceAPI;
  owner?: string;
  returnURL: string;
  memberEntry?: (instanceID: number) => { to: string; state?: object; target?: string };
  Layout?: ComponentType<ComponentProps<typeof UserLayout>>;
};
const Context = createContext<TeamPageEnvironment>({ api: teamService, returnURL: '/teams' });
export function TeamPageEnvironmentProvider({ value, children }: { value: TeamPageEnvironment; children: ReactNode }) { return <Context.Provider value={value}>{children}</Context.Provider>; }
export function useTeamPageEnvironment() { return useContext(Context); }
export function TeamPageLayout(props: ComponentProps<typeof UserLayout>) { const Layout = useTeamPageEnvironment().Layout || UserLayout; return <Layout {...props} />; }
