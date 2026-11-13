import { isAxiosError } from "axios";
import { useEffect, useState } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { Plus } from "lucide-react";
import UserLayout from "../../components/UserLayout";
import { useI18n } from "../../contexts/I18nContext";
import { forgetInstance, readLastInstance, rememberInstanceTarget, rememberLastInstance } from "../../lib/instanceNavigation";
import { instanceService } from "../../services/instanceService";
import { useAuthStore } from "../../stores/authStore";
import type { Instance } from "../../types/instance";

type EntryState = { userId?: number; status: "loading" | "empty" | "error" };

export default function InstanceWorkspaceEntry() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const { search } = useLocation();
  const userId = useAuthStore(auth => auth.isAuthenticated ? auth.user?.id : undefined);
  const [state, setState] = useState<EntryState>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (!userId) return;
    const controller = new AbortController();
    const current = () => {
      const auth = useAuthStore.getState();
      return !controller.signal.aborted && auth.isAuthenticated && auth.user?.id === userId;
    };
    const restore = async () => {
      try {
        let instance: Instance | undefined;
        const lastId = readLastInstance(userId);
        if (lastId) {
          try { instance = await instanceService.getInstance(lastId); }
          catch (error) {
            if (!current()) return;
            if (!isAxiosError(error) || ![403, 404].includes(error.response?.status ?? 0)) throw error;
            forgetInstance(userId, lastId);
          }
          if (!current()) return;
          if (instance?.status === "deleting") { forgetInstance(userId, lastId); instance = undefined; }
        }
        if (!instance) {
          for (let page = 1; !instance; page += 1) {
            const result = await instanceService.getInstances(page, 50, undefined, controller.signal);
            if (!current()) return;
            const eligible = result.instances.filter(row => row.status !== "deleting");
            instance = eligible.find(row => row.status === "running") ?? eligible[0];
            if (page * result.limit >= result.total || result.instances.length === 0) break;
          }
        }
        if (!current()) return;
        if (instance) {
          rememberInstanceTarget(userId, instance);
          rememberLastInstance(userId, instance.id);
          navigate({ pathname: `/instances/${instance.id}`, search }, { replace: true });
        } else {
          setState({ userId, status: "empty" });
        }
      } catch {
        if (current()) setState({ userId, status: "error" });
      }
    };
    void restore();
    return () => controller.abort();
  }, [userId, search, navigate, attempt]);

  const status = state.userId === userId ? state.status : "loading";

  return <UserLayout title={t("instances.listTitle")}>
    {status === "loading" ? <div role="status" className="flex h-64 items-center justify-center text-sm text-slate-500">{t("common.loading")}</div>
      : status === "error" ? <div role="alert" className="flex items-center gap-3 text-sm text-red-700">
        {t("instances.failedToLoad")}
        <button type="button" className="app-button-secondary" onClick={() => { setState({userId,status:"loading"}); setAttempt(value => value + 1); }}>{t("common.retry")}</button>
      </div>
      : <div className="flex min-h-80 flex-col items-center justify-center gap-4 text-center">
        <p className="text-sm text-slate-600">{t("instances.noInstances")}</p>
        <Link to="/instances/new" className="app-button-primary"><Plus size={18} aria-hidden="true" />{t("instances.createInstance")}</Link>
      </div>}
  </UserLayout>;
}
