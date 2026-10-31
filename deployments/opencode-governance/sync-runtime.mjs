// Uses apply_patch for every destination edit. No deploy, delete or git reset.
import fs from "node:fs"
import path from "node:path"
import { execFileSync, spawnSync } from "node:child_process"
import { fileURLToPath } from "node:url"

const source = path.dirname(fileURLToPath(import.meta.url))
const target = path.resolve(process.argv[2] ?? "")
const patchExecutable = process.argv[3]
if (!target.endsWith(path.join("opencode", "model-governance")) || !patchExecutable) throw new Error("Expected exact opencode/model-governance target and apply-patch executable")
const names = ["governance.ts", "governance.test.ts", "image-tools.ts", "image-tools.test.ts", "apply-governance.mjs", "Dockerfile", "README.md", "build-local.sh", "test-images-local.sh"]
for (const name of names) {
  const destination = path.join(target, name).replaceAll("\\", "/")
  const contents = fs.readFileSync(path.join(source, name), "utf8").replaceAll("\r\n", "\n").trimEnd()
  const exists = fs.existsSync(destination)
  const old = exists ? fs.readFileSync(destination, "utf8").replaceAll("\r\n", "\n").trimEnd() : undefined
  if (old === contents) continue
  // Send small contextual hunks instead of repeating the entire old/new file:
  // the latter exceeds Windows' command-line limit for larger test suites.
  const patches = []
  if (exists) {
    const diff = spawnSync("git", ["diff", "--no-index", "--no-ext-diff", "--no-color", "--unified=3", "--", destination, path.join(source, name)], { encoding: "utf8", maxBuffer: 8 * 1024 * 1024 })
    if (diff.error || (diff.status !== 0 && diff.status !== 1)) throw new Error(`Cannot compare ${name}: ${diff.error?.message ?? diff.stderr}`)
    for (const hunk of diff.stdout.replaceAll("\r\n", "\n").split(/^@@[^\n]*\n/m).slice(1)) {
      const lines = hunk.replace(/\n$/, "").split("\n").filter((line) => !line.startsWith("\\ No newline at end of file"))
      patches.push(`*** Begin Patch\n*** Update File: ${destination}\n@@\n${lines.join("\n")}\n*** End Patch`)
    }
  } else {
    patches.push(`*** Begin Patch\n*** Add File: ${destination}\n${contents.split("\n").map((line) => "+" + line).join("\n")}\n*** End Patch`)
  }
  for (const patch of patches) {
    try {
      execFileSync(patchExecutable, ["--codex-run-as-apply-patch", patch], { stdio: "inherit" })
    } catch {
      throw new Error(`Could not apply ${name}; stop and inspect the destination before retrying`)
    }
  }
  if (fs.readFileSync(destination, "utf8").replaceAll("\r\n", "\n").trimEnd() !== contents) throw new Error(`Verification failed for ${name}`)
}
console.log("Synced verified adapter sources; historical experimental files retained but not used by the new build")
