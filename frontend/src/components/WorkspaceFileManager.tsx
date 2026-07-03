import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Download,
  Eye,
  File,
  FileText,
  Folder,
  FolderPlus,
  FolderUp,
  Pencil,
  RefreshCw,
  Trash2,
  Upload,
  X,
} from "lucide-react";
import { workspaceService } from "../services/workspaceService";
import type { WorkspaceEntry, WorkspacePreview } from "../types/workspace";

interface WorkspaceFileManagerProps {
  instanceId: number;
  initialPath?: string;
  onMutation?: () => void | Promise<void>;
  refreshKey?: number;
}

const invalidNamePattern = /[\\/]/;

function joinPath(base: string, name: string) {
  return [base, name].filter(Boolean).join("/");
}

function parentPath(path: string) {
  const parts = path.split("/").filter(Boolean);
  parts.pop();
  return parts.join("/");
}

function fileName(path: string) {
  const parts = path.split("/").filter(Boolean);
  return parts.at(-1) ?? "";
}

function normalizeWorkspacePath(path: string | undefined) {
  const raw = path?.trim() ?? "";
  if (!raw || raw === "." || raw === "/") {
    return "";
  }
  if (raw.startsWith("/")) {
    return "";
  }
  return raw
    .replaceAll("\\", "/")
    .split("/")
    .filter(Boolean)
    .join("/");
}

function rootBreadcrumbLabel(initialPath: string | undefined) {
  const raw = initialPath?.trim() ?? "";
  if (!raw.startsWith("/")) {
    return "Workspace";
  }
  return raw.replace(/\/+$/, "") || "/";
}

function breadcrumbItems(path: string, rootLabel: string) {
  const parts = path.split("/").filter(Boolean);
  return [
    { label: rootLabel, path: "" },
    ...parts.map((part, index) => ({
      label: part,
      path: parts.slice(0, index + 1).join("/"),
    })),
  ];
}

function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB", "TB"];
  let size = value;
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex += 1;
  }
  return `${size.toFixed(size >= 10 || unitIndex === 0 ? 0 : 1)} ${units[unitIndex]}`;
}

function downloadBlob(blob: Blob, name: string) {
  const url = window.URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = name || "download";
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.URL.revokeObjectURL(url);
}

function assertEntryName(name: string) {
  const trimmed = name.trim();
  if (!trimmed || trimmed === "." || trimmed === ".." || invalidNamePattern.test(trimmed)) {
    return null;
  }
  return trimmed;
}

function normalizeUploadRelativePath(path: string) {
  const parts = path
    .replaceAll("\\", "/")
    .split("/")
    .map((part) => part.trim())
    .filter(Boolean);
  if (
    parts.length === 0 ||
    parts.some((part) => part === "." || part === ".." || invalidNamePattern.test(part))
  ) {
    return null;
  }
  return parts.join("/");
}

function collectFolderUploadDirectories(files: File[]) {
  const directories = new Set<string>();
  files.forEach((file) => {
    const relativePath = normalizeUploadRelativePath(file.webkitRelativePath || file.name);
    if (!relativePath) {
      return;
    }
    const directory = parentPath(relativePath);
    if (!directory) {
      return;
    }
    const parts = directory.split("/").filter(Boolean);
    parts.forEach((_, index) => {
      directories.add(parts.slice(0, index + 1).join("/"));
    });
  });
  return Array.from(directories).sort((left, right) => {
    const leftDepth = left.split("/").length;
    const rightDepth = right.split("/").length;
    return leftDepth === rightDepth ? left.localeCompare(right) : leftDepth - rightDepth;
  });
}

function getErrorMessage(err: unknown, fallback: string) {
  const responseError = (err as { response?: { data?: { error?: string } } })?.response?.data?.error;
  if (responseError) {
    return responseError;
  }
  return err instanceof Error ? err.message : fallback;
}

function isWorkspaceEntryExistsError(err: unknown) {
  const message = getErrorMessage(err, "").toLowerCase();
  return message.includes("already exists") || message.includes("entry already exists");
}

function EntryIcon({ entry }: { entry: WorkspaceEntry }) {
  if (entry.is_dir) {
    return <Folder className="h-4 w-4 text-slate-500" />;
  }
  if (entry.previewable) {
    return <FileText className="h-4 w-4 text-slate-500" />;
  }
  return <File className="h-4 w-4 text-slate-500" />;
}

export function WorkspaceFileManager({ instanceId, initialPath, onMutation, refreshKey }: WorkspaceFileManagerProps) {
  const queryClient = useQueryClient();
  const uploadInputRef = useRef<HTMLInputElement | null>(null);
  const folderUploadInputRef = useRef<HTMLInputElement | null>(null);
  const uploadMenuRef = useRef<HTMLDivElement | null>(null);
  const [currentPath, setCurrentPath] = useState(() => normalizeWorkspacePath(initialPath));
  const [previewPath, setPreviewPath] = useState<string | null>(null);
  const previewObjectUrlRef = useRef<string | null>(null);
  const [previewObjectUrl, setPreviewObjectUrl] = useState<string | null>(null);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [uploadStatus, setUploadStatus] = useState<string | null>(null);
  const [uploadMenuOpen, setUploadMenuOpen] = useState(false);
  const rootLabel = rootBreadcrumbLabel(initialPath);

  useEffect(() => {
    setCurrentPath(normalizeWorkspacePath(initialPath));
    setPreviewPath(null);
  }, [instanceId, initialPath]);

  useEffect(() => {
    if (!uploadMenuOpen) {
      return;
    }
    const handlePointerDown = (event: PointerEvent) => {
      if (uploadMenuRef.current?.contains(event.target as Node)) {
        return;
      }
      setUploadMenuOpen(false);
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setUploadMenuOpen(false);
      }
    };
    window.addEventListener("pointerdown", handlePointerDown);
    window.addEventListener("keydown", handleKeyDown);
    return () => {
      window.removeEventListener("pointerdown", handlePointerDown);
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [uploadMenuOpen]);

  const entriesQuery = useQuery({
    queryKey: ["workspace", instanceId, currentPath],
    queryFn: () => workspaceService.list(instanceId, currentPath),
  });

  const previewQuery = useQuery({
    queryKey: ["workspace-preview", instanceId, previewPath],
    queryFn: () => workspaceService.preview(instanceId, previewPath ?? ""),
    enabled: Boolean(previewPath),
  });

  useEffect(() => {
    let disposed = false;
    if (previewObjectUrlRef.current) {
      window.URL.revokeObjectURL(previewObjectUrlRef.current);
      previewObjectUrlRef.current = null;
      setPreviewObjectUrl(null);
    }

    const preview = previewQuery.data;
    if (!previewPath || !preview || (preview.kind !== "image" && preview.kind !== "pdf")) {
      return;
    }

    workspaceService
      .previewBlob(instanceId, previewPath)
      .then((blob) => {
        if (!disposed) {
          const url = window.URL.createObjectURL(blob);
          previewObjectUrlRef.current = url;
          setPreviewObjectUrl(url);
        }
      })
      .catch((err: unknown) => {
        if (!disposed) {
          setError(getErrorMessage(err, "Failed to load preview"));
        }
      });

    return () => {
      disposed = true;
    };
  }, [instanceId, previewPath, previewQuery.data]);

  useEffect(() => {
    return () => {
      if (previewObjectUrlRef.current) {
        window.URL.revokeObjectURL(previewObjectUrlRef.current);
        previewObjectUrlRef.current = null;
      }
    };
  }, []);

  const invalidateCurrentPath = async () => {
    await queryClient.invalidateQueries({
      queryKey: ["workspace", instanceId, currentPath],
    });
  };

  useEffect(() => {
    if (refreshKey === undefined) {
      return;
    }
    void queryClient.invalidateQueries({
      queryKey: ["workspace", instanceId, currentPath],
    });
  }, [currentPath, instanceId, queryClient, refreshKey]);

  const notifyMutation = async () => {
    await onMutation?.();
  };

  const runAction = async (key: string, action: () => Promise<void>) => {
    try {
      setBusyAction(key);
      setError(null);
      await action();
    } catch (err: unknown) {
      setError(getErrorMessage(err, "Workspace action failed"));
    } finally {
      setBusyAction(null);
      setUploadStatus(null);
    }
  };

  const clearUploadInputs = () => {
    if (uploadInputRef.current) {
      uploadInputRef.current.value = "";
    }
    if (folderUploadInputRef.current) {
      folderUploadInputRef.current.value = "";
    }
  };

  const handleUploadFiles = (fileList?: FileList | null) => {
    const files = Array.from(fileList ?? []);
    if (files.length === 0) {
      return;
    }
    void runAction("upload", async () => {
      for (const [index, file] of files.entries()) {
        setUploadStatus(`Uploading ${index + 1}/${files.length}`);
        await workspaceService.upload(instanceId, currentPath, file);
      }
      await invalidateCurrentPath();
      await notifyMutation();
      clearUploadInputs();
    });
  };

  const handleUploadFolder = (fileList?: FileList | null) => {
    const files = Array.from(fileList ?? []);
    if (files.length === 0) {
      return;
    }
    void runAction("upload-folder", async () => {
      const directories = collectFolderUploadDirectories(files);
      for (const [index, directory] of directories.entries()) {
        setUploadStatus(`Creating folders ${index + 1}/${directories.length}`);
        try {
          await workspaceService.mkdir(instanceId, joinPath(currentPath, directory));
        } catch (err: unknown) {
          if (!isWorkspaceEntryExistsError(err)) {
            throw err;
          }
        }
      }

      for (const [index, file] of files.entries()) {
        const relativePath = normalizeUploadRelativePath(file.webkitRelativePath || file.name);
        if (!relativePath) {
          continue;
        }
        setUploadStatus(`Uploading ${index + 1}/${files.length}`);
        await workspaceService.upload(instanceId, joinPath(currentPath, parentPath(relativePath)), file);
      }
      await invalidateCurrentPath();
      await notifyMutation();
      clearUploadInputs();
    });
  };

  const openFileUploadPicker = () => {
    setUploadMenuOpen(false);
    uploadInputRef.current?.click();
  };

  const openFolderUploadPicker = () => {
    setUploadMenuOpen(false);
    folderUploadInputRef.current?.click();
  };

  const handleMkdir = () => {
    const name = assertEntryName(window.prompt("Folder name") || "");
    if (!name) {
      return;
    }
    void runAction("mkdir", async () => {
      await workspaceService.mkdir(instanceId, joinPath(currentPath, name));
      await invalidateCurrentPath();
      await notifyMutation();
    });
  };

  const handleRename = (entry: WorkspaceEntry) => {
    const name = assertEntryName(window.prompt("Rename", entry.name) || "");
    if (!name || name === entry.name) {
      return;
    }
    void runAction(`rename:${entry.path}`, async () => {
      await workspaceService.rename(instanceId, entry.path, joinPath(parentPath(entry.path), name));
      if (previewPath === entry.path) {
        setPreviewPath(null);
      }
      await invalidateCurrentPath();
      await notifyMutation();
    });
  };

  const handleDelete = (entry: WorkspaceEntry) => {
    if (!window.confirm(`Delete ${entry.name}?`)) {
      return;
    }
    void runAction(`delete:${entry.path}`, async () => {
      await workspaceService.remove(instanceId, entry.path);
      if (previewPath === entry.path || previewPath?.startsWith(`${entry.path}/`)) {
        setPreviewPath(null);
      }
      await invalidateCurrentPath();
      await notifyMutation();
    });
  };

  const handleDownload = (entry: WorkspaceEntry) => {
    void runAction(`download:${entry.path}`, async () => {
      const blob = await workspaceService.downloadBlob(instanceId, entry.path);
      downloadBlob(blob, entry.name || fileName(entry.path));
    });
  };

  const preview = previewQuery.data;
  const entries = entriesQuery.data ?? [];

  return (
    <section className="cm-surface flex h-full max-h-full min-h-[420px] min-w-0 flex-col overflow-hidden xl:min-h-0">
      <div className="flex items-center justify-between gap-3 border-b border-slate-200 px-3 py-2">
        <div className="min-w-0">
          <div className="flex min-w-0 flex-wrap items-center gap-1 text-sm">
            {breadcrumbItems(currentPath, rootLabel).map((item, index, items) => (
              <span key={item.path || "root"} className="flex min-w-0 items-center gap-1">
                <button
                  type="button"
                  className="max-w-[140px] truncate rounded px-1.5 py-1 font-medium text-slate-700 hover:bg-slate-100"
                  onClick={() => setCurrentPath(item.path)}
                >
                  {item.label}
                </button>
                {index < items.length - 1 && <span className="text-slate-400">/</span>}
              </span>
            ))}
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <input
            ref={uploadInputRef}
            type="file"
            className="hidden"
            multiple
            onChange={(event) => handleUploadFiles(event.target.files)}
          />
          <input
            ref={folderUploadInputRef}
            type="file"
            className="hidden"
            multiple
            {...{ webkitdirectory: "", directory: "" }}
            onChange={(event) => handleUploadFolder(event.target.files)}
          />
          <button
            type="button"
            className="cm-icon-button"
            title="Refresh"
            onClick={() => void invalidateCurrentPath()}
          >
            <RefreshCw className={`h-4 w-4 ${entriesQuery.isFetching ? "animate-spin" : ""}`} />
          </button>
          <button type="button" className="cm-icon-button" title="New folder" onClick={handleMkdir}>
            <FolderPlus className="h-4 w-4" />
          </button>
          <div ref={uploadMenuRef} className="relative">
            <button
              type="button"
              className="cm-icon-button"
              title="Upload"
              aria-haspopup="menu"
              aria-expanded={uploadMenuOpen}
              disabled={busyAction === "upload" || busyAction === "upload-folder"}
              onClick={() => setUploadMenuOpen((open) => !open)}
            >
              <Upload className="h-4 w-4" />
            </button>
            {uploadMenuOpen && (
              <div
                role="menu"
                className="absolute right-0 top-full z-20 mt-2 w-40 rounded-md border border-slate-200 bg-white py-1 shadow-lg"
              >
                <button
                  type="button"
                  role="menuitem"
                  className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-slate-700 hover:bg-slate-50"
                  onClick={openFileUploadPicker}
                >
                  <Upload className="h-4 w-4 text-slate-500" />
                  <span>上传文件</span>
                </button>
                <button
                  type="button"
                  role="menuitem"
                  className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-slate-700 hover:bg-slate-50"
                  onClick={openFolderUploadPicker}
                >
                  <FolderUp className="h-4 w-4 text-slate-500" />
                  <span>上传文件夹</span>
                </button>
              </div>
            )}
          </div>
        </div>
      </div>

      {uploadStatus && (
        <div className="border-b border-blue-200 bg-blue-50 px-3 py-2 text-sm text-blue-700">
          {uploadStatus}
        </div>
      )}

      {error && (
        <div className="border-b border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          {error}
        </div>
      )}

      <div className="flex min-h-0 flex-1 flex-col">
        <div className={`${previewPath ? "hidden" : "min-h-0 flex-1 overflow-y-auto overflow-x-hidden"}`}>
          {entriesQuery.isLoading ? (
            <div className="flex h-48 items-center justify-center text-sm text-slate-500">
              Loading
            </div>
          ) : entries.length === 0 ? (
            <div className="flex h-48 items-center justify-center text-sm text-slate-500">
              No files
            </div>
          ) : (
            <table className="w-full table-fixed divide-y divide-slate-200 text-sm">
              <thead className="bg-slate-50 text-left text-xs font-medium uppercase tracking-normal text-slate-500">
                <tr>
                  <th className="px-3 py-2">Name</th>
                  <th className="w-14 px-2 py-2">Size</th>
                  <th className="w-28 px-2 py-2">Modified</th>
                  <th className="w-40 px-2 py-2 text-right">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100 bg-white">
                {entries.map((entry) => (
                  <tr key={entry.path} className="hover:bg-slate-50">
                    <td className="min-w-0 px-3 py-2">
                      <button
                        type="button"
                        className="flex w-full min-w-0 items-center gap-2 text-left"
                        onClick={() =>
                          entry.is_dir ? setCurrentPath(entry.path) : setPreviewPath(entry.path)
                        }
                      >
                        <span className="shrink-0">
                          <EntryIcon entry={entry} />
                        </span>
                        <span className="min-w-0 truncate font-medium text-slate-900">{entry.name}</span>
                      </button>
                    </td>
                    <td className="truncate px-2 py-2 text-slate-500">
                      {entry.is_dir ? "-" : formatBytes(entry.size)}
                    </td>
                    <td className="truncate px-2 py-2 text-slate-500">
                      {entry.modified_at ? new Date(entry.modified_at).toLocaleString() : "-"}
                    </td>
                    <td className="px-2 py-2">
                      <div className="flex justify-end gap-1">
                        {!entry.is_dir && entry.previewable && (
                          <button
                            type="button"
                            className="cm-icon-button h-8 w-8"
                            title="Preview"
                            onClick={() => setPreviewPath(entry.path)}
                          >
                            <Eye className="h-4 w-4" />
                          </button>
                        )}
                        {!entry.is_dir && entry.downloadable && (
                          <button
                            type="button"
                            className="cm-icon-button h-8 w-8"
                            title="Download"
                            onClick={() => handleDownload(entry)}
                            disabled={busyAction === `download:${entry.path}`}
                          >
                            <Download className="h-4 w-4" />
                          </button>
                        )}
                        <button
                          type="button"
                          className="cm-icon-button h-8 w-8"
                          title="Rename"
                          onClick={() => handleRename(entry)}
                          disabled={busyAction === `rename:${entry.path}`}
                        >
                          <Pencil className="h-4 w-4" />
                        </button>
                        <button
                          type="button"
                          className="cm-icon-button h-8 w-8 border-red-200 text-red-600 hover:border-red-300 hover:bg-red-50 hover:text-red-700"
                          title="Delete"
                          onClick={() => handleDelete(entry)}
                          disabled={busyAction === `delete:${entry.path}`}
                        >
                          <Trash2 className="h-4 w-4" />
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>

        <PreviewPane
          path={previewPath}
          preview={preview}
          loading={previewQuery.isLoading}
          objectUrl={previewObjectUrl}
          onClose={() => setPreviewPath(null)}
        />
      </div>
    </section>
  );
}

function PreviewPane({
  path,
  preview,
  loading,
  objectUrl,
  onClose,
}: {
  path: string | null;
  preview?: WorkspacePreview;
  loading: boolean;
  objectUrl: string | null;
  onClose: () => void;
}) {
  if (!path) {
    return (
      <aside className="hidden border-t border-slate-200 bg-slate-50 p-4 text-sm text-slate-500">
        Preview
      </aside>
    );
  }

  return (
    <aside className="flex min-h-0 flex-1 flex-col overflow-hidden border-t border-slate-200 bg-slate-50">
      <div className="flex h-11 shrink-0 items-center justify-between gap-3 border-b border-slate-200 px-3">
        <div className="min-w-0 truncate text-sm font-medium text-slate-900">{fileName(path)}</div>
        <button type="button" className="cm-icon-button h-8 w-8" title="Close" onClick={onClose}>
          <X className="h-4 w-4" />
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-3">
        {loading ? (
          <div className="flex h-full items-center justify-center text-sm text-slate-500">
            Loading
          </div>
        ) : preview?.kind === "text" ? (
          <pre className="min-w-0 max-w-full whitespace-pre-wrap break-words rounded-md border border-slate-200 bg-white p-3 font-mono text-xs leading-5 text-slate-800">
            {preview.text}
          </pre>
        ) : preview?.kind === "image" && objectUrl ? (
          <img
            src={objectUrl}
            alt={fileName(path)}
            className="mx-auto max-h-full max-w-full rounded-md border border-slate-200 bg-white object-contain"
          />
        ) : preview?.kind === "pdf" && objectUrl ? (
          <iframe
            title={fileName(path)}
            src={objectUrl}
            className="h-full min-h-[320px] w-full rounded-md border border-slate-200 bg-white"
          />
        ) : (
          <div className="flex h-full items-center justify-center text-sm text-slate-500">
            Download only
          </div>
        )}
      </div>
    </aside>
  );
}
