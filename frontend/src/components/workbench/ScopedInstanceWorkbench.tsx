import { useCallback, useRef, useState, type ComponentProps } from "react";
import { InstanceWorkbench } from "./InstanceWorkbench";
import { WorkbenchServicesProvider } from "./WorkbenchServices";
import { readWorkbenchView, writeWorkbenchView } from "./persistence";
import { workbenchReducer, type WorkbenchEvent } from "./state";
import type { WorkbenchService } from "../../services/instanceWorkbenchService";
import type { WorkspaceFileOperations } from "../WorkspaceFileManager";

type Props = ComponentProps<typeof InstanceWorkbench> & {
  scope: string;
  service: WorkbenchService;
  workspace: WorkspaceFileOperations;
  canWrite: boolean;
};

// Callers key this view by account + instance, never by a bearer credential.
// Persist only the existing sanitized view schema; terminal output and signed
// preview URLs are deliberately not retained. Restore lists existing sessions.
export function ScopedInstanceWorkbench({ scope, service, workspace, canWrite, ...props }: Props) {
  const key = `cm:workbench:scoped:v1:${scope}:${props.instanceId}`;
  const [view, setView] = useState(() => readWorkbenchView(key, props.runtimeGeneration, sessionStorage).state);
  const current = useRef(view);
  const onWorkbenchEvent = useCallback((event: WorkbenchEvent) => {
    const next = workbenchReducer(current.current, event);
    current.current = next;
    setView(next);
    writeWorkbenchView(key, { state: next, visible: true }, sessionStorage);
  }, [key]);
  return <WorkbenchServicesProvider value={{ service, workspace, scope, canWrite, workspaceKey: key }}>
    <InstanceWorkbench {...props} sharedState={view} onWorkbenchEvent={onWorkbenchEvent} />
  </WorkbenchServicesProvider>;
}
