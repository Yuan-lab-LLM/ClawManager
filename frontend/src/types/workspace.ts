export interface WorkspaceEntry {
  name: string;
  path: string;
  is_dir: boolean;
  size: number;
  modified_at: string;
  previewable: boolean;
  downloadable: boolean;
}

export interface WorkspacePreview {
  kind: "text" | "image" | "pdf" | "binary" | "download";
  content_type: string;
  text?: string;
  preview_url?: string;
  download_url?: string;
}

export interface WorkspaceSearchResponse {
  entries: WorkspaceEntry[];
  limit?: number;
}
