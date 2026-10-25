import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const source = readFileSync(
  path.resolve(scriptDir, "../src/components/WorkspaceFileManager.tsx"),
  "utf8",
);
const translations = readFileSync(
  path.resolve(scriptDir, "../src/lib/i18n.ts"),
  "utf8",
);

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

assert(
  source.includes("folderUploadInputRef"),
  "Workspace file manager must include a dedicated folder upload input.",
);

assert(
  /<input[\s\S]*ref=\{uploadInputRef\}[\s\S]*multiple/.test(source),
  "Workspace file upload input must allow selecting multiple files.",
);

assert(
  source.includes("webkitdirectory") && source.includes("directory"),
  "Workspace folder upload input must request a browser directory picker.",
);

assert(
  source.includes("webkitRelativePath"),
  "Workspace folder upload must preserve the selected folder relative paths.",
);

assert(
  source.includes("collectFolderUploadDirectories") &&
    source.includes("service.mkdir") &&
    source.includes("service.upload"),
  "Workspace folder upload must create nested directories before uploading files.",
);

assert(
  source.includes("handleUploadFiles") && source.includes("handleUploadFolder"),
  "Workspace file manager must separate multi-file and folder upload flows.",
);

assert(
  source.includes("uploadMenuOpen") && source.includes("uploadMenuRef"),
  "Workspace upload actions must be grouped behind one upload menu.",
);

assert(
  source.includes('role="menu"') &&
    source.includes('translateLabel("workspaceFileManager.uploadFiles")') &&
    source.includes('translateLabel("workspaceFileManager.uploadFolder")') &&
    translations.includes('uploadFiles: "上传文件"') &&
    translations.includes('uploadFolder: "上传文件夹"'),
  "Workspace upload menu must expose separate file and folder upload choices.",
);

assert(
  !source.includes("CloudUpload") && !source.includes('title="Upload folder"'),
  "Workspace toolbar must not render a separate folder upload icon button.",
);

console.log("Workspace folder upload contract is valid.");
