import api from "./api";
import type {
  GitRequest,
  GitResponse,
  TerminalSession,
  TerminalOutput,
  WorkbenchCapabilities,
  WorkbenchProcess,
  WorkbenchPreview,
} from "../types/workbench";
export function workbenchError(error: unknown): string {
  const data = (error as { response?: { data?: { error?: string } } })?.response
    ?.data;
  return typeof data?.error === "string"
    ? data.error
    : error instanceof Error
      ? error.message
      : String(error);
}
const base = (id: number) => `/instances/${id}/workbench`;
const config = (generation: number, signal?: AbortSignal) => ({
  params: { generation },
  signal,
  timeout: 130000,
});
export const instanceWorkbenchService = {
  async capabilities(
    id: number,
    signal?: AbortSignal,
  ): Promise<WorkbenchCapabilities> {
    return (await api.get(`${base(id)}/capabilities`, { signal })).data.data;
  },
  async git(
    id: number,
    generation: number,
    request: GitRequest,
    signal?: AbortSignal,
  ): Promise<GitResponse> {
    return (
      await api.post(`${base(id)}/git`, request, config(generation, signal))
    ).data.data;
  },
  async terminals(
    id: number,
    generation: number,
    signal?: AbortSignal,
  ): Promise<TerminalSession[]> {
    return (
      (await api.get(`${base(id)}/terminals`, config(generation, signal))).data
        .data.sessions ?? []
    );
  },
  async createTerminal(
    id: number,
    generation: number,
    cwd = "",
  ): Promise<TerminalSession> {
    return (
      await api.post(
        `${base(id)}/terminals`,
        { cwd, cols: 100, rows: 30 },
        config(generation),
      )
    ).data.data;
  },
  async output(
    id: number,
    generation: number,
    session: string,
    cursor: number,
    signal?: AbortSignal,
  ): Promise<TerminalOutput> {
    return (
      await api.get(
        `${base(id)}/terminals/${encodeURIComponent(session)}/output`,
        { ...config(generation, signal), params: { generation, cursor } },
      )
    ).data.data;
  },
  async input(
    id: number,
    generation: number,
    session: string,
    input: string,
  ): Promise<void> {
    await api.post(
      `${base(id)}/terminals/${encodeURIComponent(session)}/input`,
      { input },
      config(generation),
    );
  },
  async resize(
    id: number,
    generation: number,
    session: string,
    cols: number,
    rows: number,
  ): Promise<void> {
    await api.post(
      `${base(id)}/terminals/${encodeURIComponent(session)}/resize`,
      { cols, rows },
      config(generation),
    );
  },
  async closeTerminal(
    id: number,
    generation: number,
    session: string,
  ): Promise<TerminalSession> {
    return (
      await api.delete(
        `${base(id)}/terminals/${encodeURIComponent(session)}`,
        config(generation),
      )
    ).data.data;
  },
  async preview(
    id: number,
    generation: number,
    url: string,
  ): Promise<WorkbenchPreview> {
    return (await api.post(`${base(id)}/previews`, { url }, config(generation)))
      .data.data;
  },
  async processes(id: number, generation: number): Promise<WorkbenchProcess[]> {
    return (
      (await api.get(`${base(id)}/processes`, config(generation))).data.data
        .processes ?? []
    );
  },
  async processAction(
    id: number,
    generation: number,
    process: string,
    action: "stop" | "restart",
  ): Promise<unknown> {
    return (
      await api.post(
        `${base(id)}/processes/${encodeURIComponent(process)}/${action}`,
        {},
        config(generation),
      )
    ).data.data;
  },
};
