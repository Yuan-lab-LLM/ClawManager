import { createContext, useContext, type ReactNode } from "react";
import type { WorkspaceFileOperations } from "../WorkspaceFileManager";
import { instanceWorkbenchService, type WorkbenchService } from "../../services/instanceWorkbenchService";
import { workspaceService } from "../../services/workspaceService";
import { useAuthStore } from "../../stores/authStore";

interface WorkbenchServices {
  service: WorkbenchService;
  workspace: WorkspaceFileOperations;
  scope: string;
  canWrite: boolean;
  workspaceKey?: string;
}
const Context = createContext<WorkbenchServices | null>(null);
export function WorkbenchServicesProvider({ value, children }: { value: WorkbenchServices; children: ReactNode }) {
  return <Context.Provider value={value}>{children}</Context.Provider>;
}
export function useWorkbenchServices(): WorkbenchServices {
  const services = useContext(Context);
  const userId = useAuthStore(auth => auth.user?.id);
  return services ?? { service: instanceWorkbenchService, workspace: workspaceService, scope: `owner:${userId ?? "anonymous"}`, canWrite: true };
}
