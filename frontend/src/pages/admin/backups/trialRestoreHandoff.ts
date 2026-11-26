const handoffKind = 'clawmanager.trial-restore-handoff.v1';
const resourceName = /^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$/;

type Placement = { cluster: string; namespace: string; artifact_claim: string };
type PlacementPair = { source: Placement; target: Placement };

export type TrialRestoreHandoff = PlacementPair & {
  kind: typeof handoffKind;
  backup_id: string;
  ciphertext_sha256: string;
};

export function isTrialBackupID(value: unknown): value is string {
  return typeof value === 'string' && /^[0-9a-f]{32}$/.test(value);
}

function isCiphertextDigest(value: unknown): value is string {
  return typeof value === 'string' && /^[0-9a-f]{64}$/.test(value);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isPlacement(value: unknown): value is Placement {
  return isRecord(value) && [value.cluster, value.namespace, value.artifact_claim]
    .every((field) => typeof field === 'string' && resourceName.test(field));
}

function placement({ cluster, namespace, artifact_claim }: Placement): Placement {
  return { cluster, namespace, artifact_claim };
}

export function createTrialHandoff(pair: PlacementPair, id: unknown, digest: unknown): TrialRestoreHandoff | undefined {
  if (!isTrialBackupID(id) || !isCiphertextDigest(digest)) return;
  return {
    kind: handoffKind, backup_id: id, ciphertext_sha256: digest,
    source: placement(pair.source), target: placement(pair.target),
  };
}

export async function readTrialHandoff(file: File): Promise<TrialRestoreHandoff> {
  if (file.size > 16_384) throw new Error('Oversized trial handoff');
  const value: unknown = JSON.parse(await file.text());
  if (!isRecord(value) || value.kind !== handoffKind || !isPlacement(value.source) || !isPlacement(value.target)) {
    throw new Error('Invalid trial handoff');
  }
  const handoff = createTrialHandoff({ source: value.source, target: value.target }, value.backup_id, value.ciphertext_sha256);
  if (!handoff) throw new Error('Invalid trial handoff backup identity');
  return handoff;
}

function samePlacement(a: Placement, b: Placement) {
  return a.cluster === b.cluster && a.namespace === b.namespace && a.artifact_claim === b.artifact_claim;
}

export function matchesTrialHandoff(handoff: TrialRestoreHandoff, pair?: PlacementPair) {
  return !!pair && samePlacement(handoff.source, pair.source) && samePlacement(handoff.target, pair.target);
}

export function downloadTrialJson(filename: string, content: unknown) {
  const file = new Blob([JSON.stringify(content, null, 2) + '\n'], { type: 'application/json' });
  downloadTrialBlob(filename, file);
}

export function downloadTrialBlob(filename: string, file: Blob) {
  const url = URL.createObjectURL(file);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  link.click();
  window.setTimeout(() => URL.revokeObjectURL(url), 0);
}
