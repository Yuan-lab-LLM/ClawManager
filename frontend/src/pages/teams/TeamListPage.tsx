import TeamPowerControl, { teamPowerLabel } from '../../components/teams/TeamPowerControl';
import React, { useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import UserLayout from "../../components/UserLayout";
import { teamService } from "../../services/teamService";
import type { Team } from "../../types/team";

const statusStyle = (status: string) => {
  switch (status) {
    case "running":
      return "border-green-200 bg-green-50 text-green-700";
    case "creating":
    case "deleting":
      return "border-yellow-200 bg-yellow-50 text-yellow-700";
    case "failed":
    case "deleted":
      return "border-red-200 bg-red-50 text-red-700";
    default:
      return "border-gray-200 bg-gray-50 text-gray-700";
  }
};

const formatDateTime = (value: string) => {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
};

const TeamListPage: React.FC = () => {
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
      setError(err.response?.data?.error || "加载 Team 失败");
    } finally {
      if (!options?.silent) {
        setLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    void loadTeams();
  }, [loadTeams]);

  useEffect(() => {
    if (!teams.length) {
      return;
    }
    const timer = window.setInterval(() => {
      void loadTeams({ silent: true });
    }, 5000);
    return () => window.clearInterval(timer);
  }, [loadTeams, teams]);

  const filteredTeams = useMemo(() => {
    const query = searchQuery.trim().toLowerCase();
    if (!query) {
      return teams;
    }
    return teams.filter((team) =>
      [team.name, team.status, team.communication_mode]
        .join(" ")
        .toLowerCase()
        .includes(query),
    );
  }, [searchQuery, teams]);

  return (
    <UserLayout title="Teams">
      <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="text-sm text-gray-600">{teams.length} Teams</div>
        <div className="flex flex-col gap-3 sm:flex-row">
          <div className="relative">
            <div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3">
              <svg
                className="h-5 w-5 text-gray-400"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"
                />
              </svg>
            </div>
            <input
              value={searchQuery}
              onChange={(event) => setSearchQuery(event.target.value)}
              placeholder="搜索 Team"
              className="block w-full rounded-xl border border-[#eadfd8] bg-white py-2 pl-10 pr-3 text-sm placeholder-[#9c938e] focus:border-[#ef4444] focus:outline-none focus:ring-1 focus:ring-[#f3d2c2] sm:w-64"
            />
          </div>
          <Link to="/teams/new" className="app-button-primary">
            <svg
              className="mr-2 h-5 w-5"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M12 4v16m8-8H4"
              />
            </svg>
            创建 Team
          </Link>
        </div>
      </div>

      {loading ? (
        <div className="flex h-64 items-center justify-center text-lg text-gray-600">
          正在加载...
        </div>
      ) : error ? (
        <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-red-700">
          {error}
        </div>
      ) : filteredTeams.length === 0 ? (
        <div className="app-panel border-dashed p-12 text-center">
          <svg
            className="mx-auto h-12 w-12 text-gray-400"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M17 20h5v-2a4 4 0 00-4-4h-1M9 20H4v-2a4 4 0 014-4h1m8-4a4 4 0 10-8 0 4 4 0 008 0z"
            />
          </svg>
          <h3 className="mt-3 text-sm font-medium text-gray-900">
            还没有 Team
          </h3>
          <div className="mt-6">
            <Link to="/teams/new" className="app-button-primary">
              创建 Team
            </Link>
          </div>
        </div>
      ) : (
        <div className="app-panel overflow-hidden">
          <ul className="divide-y divide-[#f1e7e1]">
            {filteredTeams.map((team) => (
              <li key={team.id} className="px-5 py-5 hover:bg-[#fffaf6]">
                <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-3">
                      <Link
                        to={`/teams/${team.id}`}
                        className="truncate text-lg font-semibold text-[#dc2626]"
                      >
                        {team.name}
                      </Link>
                      <span
                        className={`inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-medium ${statusStyle(team.runtime_state?.state || team.status)}`}
                      >
                        {teamPowerLabel(team.runtime_state) || team.status}
                      </span>
                    </div>
                    <div className="mt-2 flex flex-wrap gap-x-5 gap-y-1 text-sm text-gray-500">
                      <span>#{team.id}</span>
                      <span>{team.communication_mode}</span>
                      <span>{team.shared_pvc_name || "PVC 待创建"}</span>
                      <span>{formatDateTime(team.created_at)}</span>
                    </div>
                  </div>
                  <TeamPowerControl state={team.runtime_state} start={() => teamService.startTeam(team.id)} onChange={() => loadTeams()}/>
                  <Link
                    to={`/teams/${team.id}`}
                    className="inline-flex items-center justify-center rounded-xl border border-[#eadfd8] bg-white px-4 py-2 text-sm font-medium text-[#5f5957] hover:bg-[#fff8f5]"
                  >
                    查看详情
                  </Link>
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}
    </UserLayout>
  );
};

export default TeamListPage;
