import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const pageSource = readFileSync(
  path.resolve(scriptDir, "../src/pages/admin/UserManagementPage.tsx"),
  "utf8",
);
const settingsSource = readFileSync(
  path.resolve(scriptDir, "../src/pages/admin/SystemSettingsPage.tsx"),
  "utf8",
);
const serviceSource = readFileSync(
  path.resolve(scriptDir, "../src/services/userService.ts"),
  "utf8",
);
const enterpriseServiceSource = readFileSync(
  path.resolve(scriptDir, "../src/services/enterpriseAuthService.ts"),
  "utf8",
);
const i18nSource = readFileSync(path.resolve(scriptDir, "../src/lib/i18n.ts"), "utf8");

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

assert(
  pageSource.includes("enterpriseAuthService.getConfig()") &&
    pageSource.includes("setEnterpriseAuthSyncRole(config.sync_role)"),
  "User management must load enterprise auth config so LDAP import can honor sync_role.",
);
assert(
  pageSource.includes("ldapSyncRoleNotice") &&
    pageSource.includes("key === 'role' && enterpriseAuthSyncRole ? null"),
  "LDAP import must hide the manual role picker when LDAP role sync is enabled.",
);
assert(
  pageSource.includes("userManagementPage.ldapRole") &&
    pageSource.includes("enterpriseAuthSyncRole ? user.role : ldapImportConfig.role"),
  "LDAP preview must show the role that will be applied during import.",
);
assert(
  pageSource.includes("DEFAULT_ADMIN_IMPORT_QUOTA") &&
    pageSource.includes("quotaForImportRole(role, ldapImportConfig, enterpriseAuthSyncRole)") &&
    pageSource.includes("userManagementPage.quota"),
  "LDAP preview must show role-specific default quotas.",
);
assert(
  serviceSource.includes("updated_count?: number") &&
    serviceSource.includes("updated_users?: Array") &&
    serviceSource.includes("role?: 'admin' | 'user'") &&
    serviceSource.includes("external_ids?: string[]") &&
    serviceSource.includes("previewLDAPUsers: async (params: LDAPPreviewRequest = {})"),
  "LDAP import service types must expose preview roles and updated user results.",
);
assert(
  pageSource.includes("selectedLDAPExternalIDs") &&
    pageSource.includes("toggleAllImportableLDAPSelection") &&
    pageSource.includes("external_ids: selectedLDAPExternalIDs") &&
    pageSource.includes("selectedLDAPImportableCount === 0"),
  "LDAP import must require explicit selected directory entries.",
);
assert(
  i18nSource.includes("ldapSyncRoleNotice") &&
    i18nSource.includes("importUpdated") &&
    i18nSource.includes("updatedAccounts") &&
    i18nSource.includes("ldapSelected"),
  "LDAP role sync import UI copy must be localized.",
);
assert(
  settingsSource.includes("function parseAdminGroupDNs(value: string)") &&
    settingsSource.includes("value.split(/\\r?\\n|;/)") &&
    !settingsSource.includes("adminGroupDNsText.split(/\\r?\\n|;|,/)"),
  "LDAP admin group DN parsing must preserve commas inside distinguished names.",
);
assert(
  enterpriseServiceSource.includes("tls_ca_file: string") &&
    enterpriseServiceSource.includes("tls_server_name: string") &&
    enterpriseServiceSource.includes("details?: Record<string, string>") &&
    settingsSource.includes("ldapTLSCAFile") &&
    settingsSource.includes("ldapTLSServerName"),
  "LDAP enterprise settings must expose TLS CA/SNI fields and status details.",
);
assert(
  i18nSource.includes("ldapTLSCAFileRequiresTLS") &&
    i18nSource.includes("ldapTLSConfigFailed"),
  "LDAP TLS diagnostics must be localized.",
);

console.log("LDAP role sync import UI source contract is valid.");
