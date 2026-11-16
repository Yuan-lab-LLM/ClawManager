import { Play, RefreshCw } from 'lucide-react';
import { useI18n } from '../contexts/I18nContext';
import type { Instance } from '../types/instance';
import AgentStateIcon from './AgentStateIcon';

export default function InstanceSleepingState({
  type,
  waking,
  disabled,
  error,
  onWake,
}: {
  type: Instance['type'];
  waking: boolean;
  disabled: boolean;
  error: string | null;
  onWake: () => void;
}) {
  const { t } = useI18n();
  return (
    <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-4 p-6 text-center">
      <AgentStateIcon
        type={type}
        state="sleeping"
        size={72}
        label={t('status.auto_sleep')}
      />
      <p className="text-sm text-slate-600" role="status">
        {t('status.auto_sleep')}
      </p>
      <button
        type="button"
        className="app-button-primary"
        onClick={onWake}
        disabled={disabled}
        aria-busy={waking}
      >
        {waking ? (
          <RefreshCw
            size={16}
            aria-hidden="true"
            className="motion-safe:animate-spin"
          />
        ) : (
          <Play size={16} aria-hidden="true" />
        )}
        {t(waking ? 'instances.wakingUp' : 'instances.wakeUp')}
      </button>
      {error && (
        <p role="alert" className="max-w-lg break-words text-sm text-red-700">
          {error}
        </p>
      )}
    </div>
  );
}
