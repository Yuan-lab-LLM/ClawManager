type Translate = (key: string) => string;

export function localizeAuthError(error: string, t: Translate) {
  const normalized = error.trim().toLowerCase();

  switch (normalized) {
    case "invalid username or password":
    case "login failed":
      return t("auth.invalidCredentials");
    case "account is disabled":
      return t("auth.accountDisabled");
    case "enterprise users must change password in the enterprise identity platform":
      return t("auth.enterprisePasswordManaged");
    default:
      return error;
  }
}
