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

Key environment variables:

- `AUTH_ENTERPRISE_ALLOW_LOCAL_FALLBACK` keeps local break-glass accounts
  available when LDAP is unavailable or the user is not found. It defaults to
  `true`; set it to `false` for LDAP-strict deployments.
- `AUTH_ENTERPRISE_SYNC_ROLE` optionally updates a provisioned user's local role
  from LDAP admin group membership at login. It defaults to `false`, so roles are
  managed in ClawManager unless explicitly enabled.
- `LDAP_HOST`, `LDAP_PORT`
- `LDAP_USE_TLS` or `LDAP_START_TLS`
- `LDAP_BIND_DN`, `LDAP_BIND_PASSWORD`
- `LDAP_BASE_DN`, `LDAP_USER_FILTER`
- `LDAP_GROUP_BASE_DN`, `LDAP_GROUP_FILTER`
- `LDAP_ADMIN_GROUP_DNS`

For production, store `LDAP_BIND_PASSWORD` in a Kubernetes Secret or equivalent
secret manager instead of committing it in plain text. Prefer `LDAP_USE_TLS=true`
for LDAPS or `LDAP_START_TLS=true` for StartTLS; enable `LDAP_SKIP_TLS_VERIFY`
only for controlled test environments.

Development fixtures:

- `deployments/docker/ldap/mock-directory.ldif` contains mock LDAP users:
  `alice`, `bob`, `carol`, `dave`, `erin`, and `frank`.
- `deployments/docker/ldap/clawmanager-ldap-whitelist.csv` imports only
  `alice`, `carol`, and `erin` as LDAP users in ClawManager.
- Expected login behavior with the fixture: `alice`, `carol`, and `erin` can log
  in with their LDAP passwords after the CSV import; `bob`, `dave`, and `frank`
  still exist in LDAP but cannot log in until imported into ClawManager.

Admin diagnostics:

- `GET /api/v1/admin/auth/enterprise/status` returns a redacted LDAP status
  summary for administrators, including dial, service bind, user search, group
  search, and configuration warnings.

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
