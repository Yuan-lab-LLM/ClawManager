import { teamStatusLabel } from "../../lib/teamI18n";
import React, { useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { Plus } from "lucide-react";
import ExpandableSearch from "../../components/ExpandableSearch";
import { useI18n } from "../../contexts/I18nContext";
import "../../components/teams/team-list.css";
import UserLayout from "../../components/UserLayout";
import { teamService } from "../../services/teamService";
import type { Team } from "../../types/team";

const formatDateTime = (value: string) => {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
};

const TeamListPage: React.FC = () => {
  const { t, locale } = useI18n();
  const [teams, setTeams] = useState<Team[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState("");

  const loadTeams = useCallback(async (options?: { silent?: boolean }) => {
    try {
      if (!options?.silent) {
        setLoading(true);
      }
      setError(null);
      const data = await teamService.listTeams(1, 100);
      setTeams(data.teams || []);
    } catch (err: any) {
      setError(err.response?.data?.error || t("teams.loadFailed"));
    } finally {
      if (!options?.silent) {
        setLoading(false);
      }
    }
  }, [t]);

  useEffect(() => {
    void loadTeams();
    const update = () => { void loadTeams({ silent: true }); };
    window.addEventListener("focus", update);
    window.addEventListener("online", update);
    return () => { window.removeEventListener("focus", update); window.removeEventListener("online", update); };
  }, [loadTeams]);

  useEffect(() => {
    if (!teams.some(team => ["creating", "deleting"].includes(team.status))) return;
    const timer = window.setInterval(() => { void loadTeams({ silent: true }); }, 5000);
    return () => window.clearInterval(timer);
  }, [loadTeams, teams]);

  const filteredTeams = useMemo(() => {
    const query = searchQuery.trim().toLowerCase();
    return query ? teams.filter(team => [team.name, team.id, team.status, team.communication_mode].join(" ").toLowerCase().includes(query)) : teams;
  }, [searchQuery, teams]);

  return (
    <UserLayout title={t("nav.teams")}>
      <section className="app-panel cm-team-list" aria-label={t("nav.teams")}>
        <div className="cm-management-toolbar">
          <div className="cm-management-toolbar-filters"><ExpandableSearch value={searchQuery} onChange={setSearchQuery} label={t("teams.search")} placeholder={t("teams.searchPlaceholder")} /></div>
          <div className="cm-management-toolbar-actions"><Link to="/teams/new" className="app-button-primary"><Plus size={15} />{t("teams.create")}</Link></div>
        </div>
        {error && <div className="cm-team-list-error" role="alert">{error}<button type="button" className="app-button-secondary" onClick={() => void loadTeams()}>{t("common.retry")}</button></div>}
        <div className="cm-management-table-wrap" tabIndex={0} role="region" aria-label={t("nav.teams")}>
          <table className="cm-management-table cm-team-table">
            <colgroup><col style={{ width: '30%' }} /><col style={{ width: '12%' }} /><col style={{ width: '16%' }} /><col style={{ width: '17%' }} /><col style={{ width: '15%' }} /><col style={{ width: '10%' }} /></colgroup>
            <thead><tr>{['name', 'status', 'communication', 'storage', 'created', 'actions'].map(key => <th key={key} scope="col">{t(`teams.${key}`)}</th>)}</tr></thead>
            <tbody>{loading ? <tr><td colSpan={6} className="cm-team-empty" role="status">{t("teams.loading")}</td></tr> : !filteredTeams.length ? <tr><td colSpan={6} className="cm-team-empty">{t(searchQuery ? "teams.noMatches" : "teams.empty")}</td></tr> : filteredTeams.map(team => <tr key={team.id}>
              <td><div className="cm-team-identity"><Link className="cm-management-identity cm-management-truncate" title={team.name} to={`/teams/${team.id}`}>{team.name}</Link><span>#{team.id}</span></div></td>
              <td><span className="cm-team-status" data-status={team.status}>{teamStatusLabel(locale, team.status)}</span></td>
              <td title={team.communication_mode}><span className="cm-management-truncate cm-team-cell">{team.communication_mode}</span></td>
              <td title={team.shared_pvc_name || t("teams.storagePending")}><span className="cm-management-truncate cm-team-cell">{team.shared_pvc_name || t("teams.storagePending")}</span></td>
              <td>{formatDateTime(team.created_at)}</td>
              <td><Link className="cm-team-entry" aria-label={`${t("teams.open")} ${team.name}`} to={`/teams/${team.id}`}>{t("teams.open")}</Link></td>
            </tr>)}</tbody>
          </table>
        </div>
        <footer className="cm-management-pagination">{t("teams.count", { count: filteredTeams.length })}</footer>
      </section>
    </UserLayout>
  );
};

export default TeamListPage;
