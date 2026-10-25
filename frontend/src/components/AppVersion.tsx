import React, { useEffect, useState } from 'react';
import {
  versionService,
  type ClawManagerBuildInfo,
} from '../services/versionService';

const UNKNOWN_BUILD_VALUE = 'unknown';

const AppVersion: React.FC = () => {
  const [buildInfo, setBuildInfo] = useState<ClawManagerBuildInfo | null>(null);

  useEffect(() => {
    let active = true;
    versionService
      .get()
      .then((info) => {
        if (active) {
          setBuildInfo(info);
        }
      })
      .catch(() => {
        if (active) {
          setBuildInfo(null);
        }
      });

    return () => {
      active = false;
    };
  }, []);

  const details = buildInfo
    ? [
        `ClawManager ${buildInfo.version}`,
        buildInfo.commit !== UNKNOWN_BUILD_VALUE
          ? `Commit ${buildInfo.commit}`
          : null,
        buildInfo.build_time !== UNKNOWN_BUILD_VALUE
          ? `Built ${buildInfo.build_time}`
          : null,
      ]
        .filter(Boolean)
        .join('\n')
    : 'ClawManager version unavailable';

  return (
    <span
      className="block max-w-full truncate rounded border border-slate-200 bg-slate-50 px-1.5 py-0.5 font-mono text-[10px] font-medium leading-none text-slate-500"
      title={details}
      data-testid="clawmanager-version"
    >
      {buildInfo?.version || '—'}
    </span>
  );
};

export default AppVersion;
