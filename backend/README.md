# ClawReef Backend

ClawReef virtual desktop management platform backend API.

## Tech Stack

- Golang 1.21+
- Gin 1.9+
- upper/db 4.x
- MySQL 8.0+
- JWT Authentication

## Quick Start

### Prerequisites

- Go 1.21 or higher
- MySQL 8.0+
- Docker (optional)

### Development Setup

1. **Install dependencies**
   ```bash
   make deps
   ```

2. **Start MySQL with Docker**
   ```bash
   make docker-up
   ```

3. **Run database migration**
   ```bash
   make migrate
   ```

4. **Start the server**
   ```bash
   make run
   ```

### API Endpoints

Server runs on port **9001**.

#### Authentication
- `POST /api/v1/auth/register` - User registration
- `POST /api/v1/auth/login` - User login
- `POST /api/v1/auth/refresh` - Refresh token
- `POST /api/v1/auth/logout` - User logout
- `GET /api/v1/auth/me` - Get current user

#### Enterprise LDAP Authentication

Set `AUTH_ENTERPRISE_ENABLED=true` to let ClawManager authenticate users through
LDAP/OpenLDAP while keeping local JWT, RBAC, quota, and instance ownership flows.
LDAP users must be provisioned in the local ClawManager user list before they can
log in. A user that exists in LDAP but has not been imported into ClawManager is
rejected with the same invalid-login response as a bad username or password.
LDAP users must change their password in the enterprise identity platform.

Local users log in with their normal username, such as `fsmith`. LDAP users use
the generated `ldap_<uid>` alias; when the uid is duplicated across OUs, the
alias includes the OU, such as `ldap_fsmith_contractors`. The alias is stored
and remains stable, while the full LDAP DN is retained only as the internal
external identity. An unqualified username always selects local authentication;
it is never looked up in LDAP. Local usernames beginning with `ldap_` are
reserved so they cannot be confused with LDAP aliases.

Use the administrator's LDAP import flow or CSV import to provision LDAP users.
The LDAP import flow can preview a filtered, size-limited directory result and
imports only the selected entries. For CSV import, set `Auth Provider` to `ldap`
and provide the LDAP DN in the `External ID` column; the password column is
ignored for those rows. LDAP users use the generated stable alias (including an
OU suffix when needed) to log in. The manual create-user form remains
local-user-only because it does not collect the LDAP external identity. Existing
LDAP records without an alias are shown as pending alias completion; re-running
the LDAP import assigns the stable alias.

Key environment variables:

- `AUTH_ENTERPRISE_ALLOW_LOCAL_FALLBACK` is retained for configuration
  compatibility and has no effect on login selection. Unqualified names are
  always local; only an `ldap_` alias selects LDAP and it is authenticated by
  the stored LDAP DN.
- `AUTH_ENTERPRISE_SYNC_ROLE` optionally sets and updates provisioned LDAP users'
  local roles from LDAP admin group membership during LDAP import and login. It
  defaults to `false`, so roles are managed in ClawManager unless explicitly
  enabled.
- `LDAP_HOST`, `LDAP_PORT`
- `LDAP_USE_TLS` or `LDAP_START_TLS`
- `LDAP_TLS_CA_FILE` for an internal CA bundle mounted into the backend
  container, and `LDAP_TLS_SERVER_NAME` when the LDAP address differs from the
  certificate DNS name.
- `LDAP_BIND_DN`, `LDAP_BIND_PASSWORD`
- `AUTH_CONFIG_ENCRYPTION_KEY` when saving a bind password through the admin UI
- `LDAP_BASE_DN`, `LDAP_USER_FILTER`
- `LDAP_GROUP_BASE_DN`, `LDAP_GROUP_FILTER`
- `LDAP_ADMIN_GROUP_DNS`

`LDAP_BIND_PASSWORD` remains environment-managed when it is supplied through
the environment, so saving other LDAP settings does not require an encryption
key or copy the password into the database. `AUTH_CONFIG_ENCRYPTION_KEY` (or
`auth.configEncryptionKey` in a config file) is required only when saving a new
bind password through the administrator UI. For production, generate it with
`openssl rand -base64 32`, store it in the deployment Secret, and keep it stable
across service restarts and replicas.
Rotating it requires migrating existing database-managed bind passwords first;
otherwise the old ciphertext cannot be decrypted.
If neither value is configured, ClawManager derives a stable encryption key from
`JWT_SECRET` so existing deployments can still save a bind password; configure a
dedicated encryption key for production deployments.

For production, store `LDAP_BIND_PASSWORD` in a Kubernetes Secret or equivalent
secret manager instead of committing it in plain text. Prefer `LDAP_USE_TLS=true`
for LDAPS or `LDAP_START_TLS=true` for StartTLS. Mount the directory CA bundle
and point `LDAP_TLS_CA_FILE` at it instead of enabling `LDAP_SKIP_TLS_VERIFY`;
use `LDAP_TLS_SERVER_NAME` if the service name used for routing does not match
the certificate SAN. Enable `LDAP_SKIP_TLS_VERIFY` only for controlled test
environments. LDAP import prefetches administrator group members when
`LDAP_GROUP_FILTER` is the simple `(member=%s)` shape; more complex group
filters still work, but role detection falls back to a per-user group search.

Development fixtures:

- `deployments/docker/ldap/mock-directory.ldif` contains mock LDAP users:
  `alice`, `bob`, `carol`, `dave`, `erin`, and `frank`.
- `deployments/docker/ldap/clawmanager-ldap-whitelist.csv` is a CSV import
  example for the fixture users `alice`, `carol`, and `erin`.
- Expected login behavior with the fixture: `ldap_alice`, `ldap_carol`, and
  `ldap_erin` can log in with their LDAP passwords after either the CSV import
  or the dedicated LDAP import; `bob`, `dave`, and `frank` still exist in LDAP
  but cannot log in until imported into ClawManager.

Admin diagnostics:

- `GET /api/v1/admin/auth/enterprise/status` returns a redacted LDAP status
  summary for administrators, including dial, service bind, user search, group
  search, TLS mode, certificate verification mode, role lookup strategy, and
  configuration warnings.

### Default Admin Account

- Username: `admin`
- Password: `admin123`

## Project Structure

```
backend/
├── cmd/server/          # Application entry point
├── internal/
│   ├── config/          # Configuration
│   ├── db/              # Database connection & migrations
│   ├── models/          # Data models
│   ├── repository/      # Data access layer
│   ├── services/        # Business logic
│   ├── handlers/        # HTTP handlers
│   ├── middleware/      # HTTP middleware
│   └── utils/           # Utilities
├── deployments/         # Docker & K8s configs
└── configs/             # Configuration files
```

## Make Commands

- `make build` - Build the binary
- `make run` - Run the server
- `make test` - Run tests
- `make fmt` - Format code
- `make lint` - Run linter
- `make docker-up` - Start MySQL container
- `make migrate` - Run database migrations
