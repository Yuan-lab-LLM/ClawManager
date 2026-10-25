import { readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const source = (relativePath) =>
  readFileSync(path.resolve(scriptDir, relativePath), 'utf8');

const versionComponent = source('../src/components/AppVersion.tsx');
const versionService = source('../src/services/versionService.ts');
const userLayout = source('../src/components/UserLayout.tsx');
const adminLayout = source('../src/components/AdminLayout.tsx');

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

assert(
  /api\s*\.get\('\/version'\)/.test(versionService),
  'Version service must read the running backend build information.',
);
assert(
  versionComponent.includes('data-testid="clawmanager-version"') &&
    versionComponent.includes('buildInfo?.version'),
  'Version component must render the running ClawManager version.',
);
assert(
  userLayout.includes('<AppVersion />') && adminLayout.includes('<AppVersion />'),
  'User and admin layouts must expose the ClawManager version.',
);
assert(
  userLayout.includes('className="mt-1 max-w-40"') &&
    adminLayout.includes('className="mt-1 max-w-40"') &&
    versionComponent.includes('block max-w-full truncate'),
  'Version badges must sit below the product name without overflowing the sidebar.',
);

console.log('ClawManager version display contract is valid.');
