type Translate = (key: string) => string;

const ENTERPRISE_AUTH_ERROR_KEYS: Array<[string, string]> = [
  ['ldap service bind failed', 'enterpriseAuthErrors.ldapServiceBindFailed'],
  ['ldap dial failed', 'enterpriseAuthErrors.ldapDialFailed'],
  ['ldap starttls failed', 'enterpriseAuthErrors.ldapStartTLSFailed'],
  ['ldap user search failed', 'enterpriseAuthErrors.ldapUserSearchFailed'],
  ['ldap group search failed', 'enterpriseAuthErrors.ldapGroupSearchFailed'],
  ['ldap authentication is not enabled', 'enterpriseAuthErrors.ldapAuthenticationDisabled'],
  ['ldap import is unavailable', 'enterpriseAuthErrors.ldapImportUnavailable'],
  ['ldap diagnostics unavailable', 'enterpriseAuthErrors.ldapDiagnosticsUnavailable'],
  ['ldap test failed', 'enterpriseAuthErrors.ldapTestFailed'],
  ['ldap host is required', 'enterpriseAuthErrors.ldapHostRequired'],
  ['ldap basedn is required', 'enterpriseAuthErrors.ldapBaseDNRequired'],
  ['ldap usetls and starttls cannot both be enabled', 'enterpriseAuthErrors.ldapTLSModeConflict'],
  ['ldap userfilter must contain %s placeholder', 'enterpriseAuthErrors.ldapUserFilterPlaceholderRequired'],
  ['ldap groupfilter must contain %s placeholder', 'enterpriseAuthErrors.ldapGroupFilterPlaceholderRequired'],
  ['ldap entry has no username attribute', 'enterpriseAuthErrors.ldapUsernameAttributeMissing'],
  ['ldap username is required', 'enterpriseAuthErrors.ldapUsernameRequired'],
  ['ldap dn is required', 'enterpriseAuthErrors.ldapDNRequired'],
  ['external id is required for ldap users', 'enterpriseAuthErrors.ldapExternalIDRequired'],
  ['auth provider must be local or ldap', 'enterpriseAuthErrors.authProviderInvalid'],
  ['ldap users must be imported from ldap', 'enterpriseAuthErrors.ldapUsersMustBeImported'],
  ['local usernames cannot start with ldap_', 'enterpriseAuthErrors.localLDAPUsernameReserved'],
  ['unknown ldap import error', 'enterpriseAuthErrors.unknownLDAPImportError'],
  ['user already exists', 'enterpriseAuthErrors.userAlreadyExists'],
  ['username already exists', 'enterpriseAuthErrors.usernameAlreadyExists'],
  ['email already exists', 'enterpriseAuthErrors.emailAlreadyExists'],
  ['auth_config_encryption_key is required to save ldap bind password', 'enterpriseAuthErrors.ldapBindPasswordSaveKeyRequired'],
  ['auth_config_encryption_key is required to load ldap bind password', 'enterpriseAuthErrors.ldapBindPasswordLoadKeyRequired'],
  ['failed to decode ldap bind password', 'enterpriseAuthErrors.ldapBindPasswordDecodeFailed'],
  ['ldap bind password ciphertext is invalid', 'enterpriseAuthErrors.ldapBindPasswordCiphertextInvalid'],
  ['failed to decrypt ldap bind password', 'enterpriseAuthErrors.ldapBindPasswordDecryptFailed'],
  ['failed to generate enterprise auth nonce', 'enterpriseAuthErrors.enterpriseAuthNonceFailed'],
  ['enterprise auth settings are unavailable', 'enterpriseAuthErrors.enterpriseAuthSettingsUnavailable'],
  ['ldap_skip_tls_verify_enabled', 'enterpriseAuthErrors.ldapSkipTLSVerifyEnabled'],
  ['ldap_tls_ca_file_unused', 'enterpriseAuthErrors.ldapTLSCAFileUnused'],
  ['ldap tls ca file requires usetls or starttls', 'enterpriseAuthErrors.ldapTLSCAFileRequiresTLS'],
  ['ldap tls config failed', 'enterpriseAuthErrors.ldapTLSConfigFailed'],
];

export function localizeEnterpriseAuthIssue(issue: string | undefined | null, t: Translate) {
  const text = issue?.trim();
  if (!text) {
    return '';
  }

  const normalized = text.toLowerCase();
  const match = ENTERPRISE_AUTH_ERROR_KEYS.find(([needle]) => normalized.includes(needle));
  return match ? t(match[1]) : text;
}

export function localizeEnterpriseAuthIssues(issues: string[] | undefined | null, t: Translate) {
  return (issues || [])
    .map((issue) => localizeEnterpriseAuthIssue(issue, t))
    .filter(Boolean);
}
