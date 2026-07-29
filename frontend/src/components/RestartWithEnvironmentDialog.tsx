import React, { useEffect, useRef, useState } from "react";
import { Pencil, Plus, RotateCw, Trash2, Undo2, X } from "lucide-react";
import { useI18n } from "../contexts/I18nContext";

type EnvironmentRow = {
  id: number;
  name: string;
  value: string;
};

interface RestartWithEnvironmentDialogProps {
  open: boolean;
  instanceName: string;
  loading: boolean;
  existingNames: string[];
  existingLoading: boolean;
  existingError?: string | null;
  serverError?: string | null;
  onCancel: () => void;
  onRetryExisting: () => void;
  onConfirm: (
    environmentOverrides: Record<string, string>,
    environmentOverrideRemovals: string[],
  ) => void;
}

const ENV_NAME_PATTERN = /^[A-Za-z_][A-Za-z0-9_]*$/;

const RestartWithEnvironmentDialog: React.FC<
  RestartWithEnvironmentDialogProps
> = ({
  open,
  instanceName,
  loading,
  existingNames,
  existingLoading,
  existingError,
  serverError,
  onCancel,
  onRetryExisting,
  onConfirm,
}) => {
  const { t } = useI18n();
  const nextRowID = useRef(1);
  const [rows, setRows] = useState<EnvironmentRow[]>([
    { id: 0, name: "", value: "" },
  ]);
  const [removedNames, setRemovedNames] = useState<string[]>([]);
  const [validationError, setValidationError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) {
      return;
    }
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !loading) {
        onCancel();
      }
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [loading, onCancel, open]);

  if (!open) {
    return null;
  }

  const addRow = (name = "") => {
    setRows((current) => [
      ...current,
      { id: nextRowID.current++, name, value: "" },
    ]);
  };

  const resetExisting = (name: string) => {
    setRemovedNames((current) => current.filter((item) => item !== name));
    setRows((current) => {
      if (current.some((row) => row.name.trim() === name)) {
        return current;
      }
      return [...current, { id: nextRowID.current++, name, value: "" }];
    });
    setValidationError(null);
  };

  const markExistingForRemoval = (name: string) => {
    setRows((current) => current.filter((row) => row.name.trim() !== name));
    setRemovedNames((current) =>
      current.includes(name) ? current : [...current, name],
    );
    setValidationError(null);
  };

  const undoExistingRemoval = (name: string) => {
    setRemovedNames((current) => current.filter((item) => item !== name));
    setValidationError(null);
  };

  const updateRow = (
    id: number,
    field: "name" | "value",
    value: string,
  ) => {
    setRows((current) =>
      current.map((row) => (row.id === id ? { ...row, [field]: value } : row)),
    );
    setValidationError(null);
  };

  const removeRow = (id: number) => {
    setRows((current) => current.filter((row) => row.id !== id));
    setValidationError(null);
  };

  const submit = () => {
    const overrides: Record<string, string> = {};
    for (const row of rows) {
      const name = row.name.trim();
      if (!name && !row.value) {
        continue;
      }
      if (!name) {
        setValidationError(t("instances.customEnvNameRequired"));
        return;
      }
      if (!ENV_NAME_PATTERN.test(name)) {
        setValidationError(t("instances.invalidEnvName", { name }));
        return;
      }
      if (Object.prototype.hasOwnProperty.call(overrides, name)) {
        setValidationError(t("instances.duplicateEnvName", { name }));
        return;
      }
      if (removedNames.includes(name)) {
        setValidationError(
          t("instances.restartEnvironmentChangeConflict", { name }),
        );
        return;
      }
      overrides[name] = row.value;
    }

    if (Object.keys(overrides).length === 0 && removedNames.length === 0) {
      setValidationError(t("instances.restartEnvironmentAtLeastOne"));
      return;
    }
    onConfirm(overrides, removedNames);
  };

  const visibleError = validationError || serverError;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-[rgba(15,23,42,0.48)] px-4 py-6"
      onClick={(event) => {
        if (event.target === event.currentTarget && !loading) {
          onCancel();
        }
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="restart-with-environment-title"
        className="flex max-h-[min(760px,calc(100vh-3rem))] w-full max-w-2xl flex-col overflow-hidden rounded-[24px] border border-slate-200 bg-white shadow-[0_36px_100px_-48px_rgba(15,23,42,0.62)]"
      >
        <div className="flex items-start justify-between gap-4 border-b border-slate-200 px-6 py-5">
          <div className="min-w-0">
            <h2
              id="restart-with-environment-title"
              className="text-xl font-semibold tracking-[-0.02em] text-slate-950"
            >
              {t("instances.restartWithEnvironmentTitle")}
            </h2>
            <p className="mt-1 truncate text-sm text-slate-500">
              {instanceName}
            </p>
          </div>
          <button
            type="button"
            aria-label={t("common.close")}
            disabled={loading}
            onClick={onCancel}
            className="rounded-md p-2 text-slate-500 transition hover:bg-slate-100 hover:text-slate-900 disabled:opacity-50"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="min-h-0 overflow-y-auto px-6 py-5">
          <p className="text-sm leading-6 text-slate-600">
            {t("instances.restartWithEnvironmentDescription")}
          </p>
          <div className="mt-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800">
            {t("instances.restartWithEnvironmentSecurityNote")}
          </div>

          <section className="mt-5 rounded-xl border border-slate-200 bg-white">
            <div className="flex items-center justify-between gap-3 border-b border-slate-200 px-4 py-3">
              <div>
                <h3 className="text-sm font-semibold text-slate-900">
                  {t("instances.configuredEnvironmentVariables")}
                </h3>
                <p className="mt-0.5 text-xs text-slate-500">
                  {t("instances.configuredEnvironmentVariablesCount", {
                    count: existingNames.length,
                  })}
                </p>
              </div>
            </div>

            {existingLoading ? (
              <div className="flex items-center gap-2 px-4 py-4 text-sm text-slate-500">
                <RotateCw className="h-4 w-4 animate-spin" />
                {t("instances.configuredEnvironmentLoading")}
              </div>
            ) : existingError ? (
              <div className="flex items-center justify-between gap-3 px-4 py-4">
                <p className="text-sm text-red-700">{existingError}</p>
                <button
                  type="button"
                  disabled={loading}
                  onClick={onRetryExisting}
                  className="app-button-secondary shrink-0"
                >
                  {t("common.retry")}
                </button>
              </div>
            ) : existingNames.length === 0 ? (
              <p className="px-4 py-4 text-sm text-slate-500">
                {t("instances.configuredEnvironmentEmpty")}
              </p>
            ) : (
              <div className="max-h-56 divide-y divide-slate-100 overflow-y-auto">
                {existingNames.map((name) => {
                  const pendingRemoval = removedNames.includes(name);
                  const pendingReset = rows.some(
                    (row) => row.name.trim() === name,
                  );
                  return (
                    <div
                      key={name}
                      className={`flex flex-wrap items-center justify-between gap-3 px-4 py-3 ${
                        pendingRemoval ? "bg-red-50/70" : ""
                      }`}
                    >
                      <div className="min-w-0">
                        <p
                          className={`truncate font-mono text-sm font-medium ${
                            pendingRemoval
                              ? "text-red-700 line-through"
                              : "text-slate-900"
                          }`}
                        >
                          {name}
                        </p>
                        <p className="mt-0.5 text-xs text-slate-500">
                          {pendingRemoval
                            ? t("instances.environmentVariableWillBeRemoved")
                            : t("instances.configuredEnvironmentValueHidden")}
                        </p>
                      </div>
                      {pendingRemoval ? (
                        <button
                          type="button"
                          disabled={loading}
                          onClick={() => undoExistingRemoval(name)}
                          className="app-button-secondary"
                        >
                          <Undo2 className="h-4 w-4" />
                          {t("instances.undoEnvironmentVariableRemoval")}
                        </button>
                      ) : (
                        <div className="flex items-center gap-2">
                          <button
                            type="button"
                            disabled={loading || pendingReset}
                            onClick={() => resetExisting(name)}
                            className="app-button-secondary"
                          >
                            <Pencil className="h-4 w-4" />
                            {pendingReset
                              ? t("instances.resetEnvironmentVariablePending")
                              : t("instances.resetEnvironmentVariable")}
                          </button>
                          <button
                            type="button"
                            aria-label={t(
                              "instances.removeEnvironmentVariable",
                              { name },
                            )}
                            disabled={loading}
                            onClick={() => markExistingForRemoval(name)}
                            className="rounded-md border border-red-200 bg-white p-2.5 text-red-600 transition hover:bg-red-50 hover:text-red-700 disabled:opacity-50"
                          >
                            <Trash2 className="h-4 w-4" />
                          </button>
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            )}
          </section>

          <h3 className="mt-5 text-sm font-semibold text-slate-900">
            {t("instances.newOrUpdatedEnvironmentVariables")}
          </h3>
          <div className="mt-5 space-y-3">
            {rows.map((row) => (
              <div
                key={row.id}
                className="grid gap-3 rounded-lg border border-slate-200 bg-slate-50 p-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.35fr)_auto]"
              >
                <label className="block">
                  <span className="mb-1 block text-xs font-medium text-slate-600">
                    {t("instances.variable")}
                  </span>
                  <input
                    type="text"
                    value={row.name}
                    disabled={loading}
                    onChange={(event) =>
                      updateRow(row.id, "name", event.target.value)
                    }
                    placeholder={t("instances.variableNamePlaceholder")}
                    autoComplete="off"
                    spellCheck={false}
                    className="app-input w-full font-mono"
                  />
                </label>
                <label className="block">
                  <span className="mb-1 block text-xs font-medium text-slate-600">
                    {t("instances.overrideValue")}
                  </span>
                  <input
                    type="text"
                    value={row.value}
                    disabled={loading}
                    onChange={(event) =>
                      updateRow(row.id, "value", event.target.value)
                    }
                    placeholder={t("instances.variableValuePlaceholder")}
                    autoComplete="off"
                    spellCheck={false}
                    className="app-input w-full font-mono"
                  />
                </label>
                <button
                  type="button"
                  aria-label={t("instances.remove")}
                  disabled={loading}
                  onClick={() => removeRow(row.id)}
                  className="self-end rounded-md border border-slate-200 bg-white p-2.5 text-slate-500 transition hover:border-red-200 hover:bg-red-50 hover:text-red-700 disabled:opacity-50"
                >
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
            ))}
          </div>

          <button
            type="button"
            disabled={loading}
            onClick={() => addRow()}
            className="app-button-secondary mt-3"
          >
            <Plus className="h-4 w-4" />
            {t("instances.addVariable")}
          </button>

          {visibleError && (
            <div
              role="alert"
              className="mt-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700"
            >
              {visibleError}
            </div>
          )}
        </div>

        <div className="flex justify-end gap-3 border-t border-slate-200 bg-slate-50 px-6 py-4">
          <button
            type="button"
            disabled={loading}
            onClick={onCancel}
            className="app-button-secondary"
          >
            {t("common.cancel")}
          </button>
          <button
            type="button"
            disabled={loading}
            onClick={submit}
            className="app-button-primary"
          >
            <RotateCw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} />
            {loading
              ? t("instances.restarting")
              : t("instances.saveAndRestart")}
          </button>
        </div>
      </div>
    </div>
  );
};

export default RestartWithEnvironmentDialog;
