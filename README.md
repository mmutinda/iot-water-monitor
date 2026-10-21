# IoT Water Monitor

A React dashboard and Go API for monitoring water-tank levels, temperature,
humidity, and pump state. This portfolio snapshot demonstrates custom JWT
authentication and server-enforced role-based authorization alongside an IoT
monitoring application.

Only the web client and backend are included. Firmware, mobile clients,
production settings, runtime data, and the original repository history are
not included. All connection defaults point to local services.

## Authentication

1. The React login form sends credentials to `POST /login`.
2. Go compares them with two environment-configured accounts.
3. A successful login returns an HS256-signed JWT valid for 24 hours.
4. The browser stores the JWT and role in `localStorage`.
5. `authFetch()` sends `Authorization: Bearer <token>` on API requests.
6. Middleware verifies the JWT and passes its role to the handler.
7. Pump commands and configuration updates additionally require `superadmin`.

| Access | Viewer | Superadmin |
|---|---|---|
| Tank readings, analytics, environment data | Yes | Yes |
| Pump state and current configuration | Yes | Yes |
| Start/stop the pump | No | Yes |
| Update pump configuration | No | Yes |

Live updates use `/events?token=<jwt>` because browser `EventSource` cannot
attach arbitrary headers. Authentication occurs when the connection opens.

Key implementation locations:

- `waterLevelServer/main.go`: `loginHandler`, `authMiddleware`,
  `sseAuthMiddleware`, and `requireSuperadmin`.
- `waterLevelClient/src/components/LoginPage.jsx`: credential submission.
- `waterLevelClient/src/utils/auth.js`: storage and bearer-authenticated fetch.
- `waterLevelClient/src/utils/sseClient.js`: authenticated event stream.
- `waterLevelServer/main_test.go`: focused authentication and permission tests.

## Run locally

Requirements: Go 1.24 or later, Node.js 22 LTS, npm, MySQL 8, and a local MQTT
broker such as Mosquitto. Hardware is not required for login and authorization;
sample readings can be published to the local broker.

### 1. Initialize a local database

From the repository root:

```bash
mysql -u root -p -e \
  "CREATE DATABASE IF NOT EXISTS water_sensor CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"
mysql -u root -p water_sensor < waterLevelServer/db/schema.sql
mysql -u root -p water_sensor < waterLevelServer/db/demo_tanks.sql
```

`schema.sql` creates the current schema. Files under `db/migrations/` are for
upgrading older installations; do not run them after creating a fresh schema.
`demo_tanks.sql` contains synthetic configuration only, not recorded readings.

### 2. Configure the backend

```bash
cp .env.example .env
```

Edit `.env` with your local database password, your own admin/viewer passwords,
and any local MQTT credentials. Never use a production database or broker for
this demonstration. Keep `.env` private; it is ignored by Git.

In a shell at the repository root:

```bash
set -a
. ./.env
set +a
export JWT_SECRET="$(openssl rand -hex 32)"
cd waterLevelServer
go run .
```

The generated signing key is local to that shell. Restarting with a different
key invalidates old tokens on subsequent requests. For a persistent local key,
save your own randomly generated value privately in `.env`.

The API listens on port 5001. The server exits if `JWT_SECRET` is missing or
blank. An account with an empty password is disabled. Usernames default to
`admin` and `viewer`.

Start your local MQTT broker separately on port 1883. Local broker access
configuration depends on your installation. The API uses `MQTT_USER` and
`MQTT_PASS` if configured.

### 3. Start the dashboard

In another terminal:

```bash
cd waterLevelClient
npm ci
npm run dev
```

Open `http://localhost:3000` and sign in with your configured credentials.
Vite proxies API calls, including `/login` and `/events`, to localhost:5001.

To send a synthetic tank reading using the Mosquitto client:

```bash
mosquitto_pub -h localhost -p 1883 -t tank_a/level \
  -m '{"distance_cm":75}'
```

Supply your local MQTT credentials if the broker requires them. The server
subscribes to each tank's configured topic and broadcasts received readings.

## Checks

```bash
cd waterLevelServer
go test -race ./...
go vet ./...
go build -o bin/water-level-server .
```

The authentication tests run without MySQL, an MQTT broker, or hardware.

```bash
cd waterLevelClient
npm ci
npm run build
```

Vite produces the frontend bundle in `dist/`. The Go server serves API routes,
not the frontend bundle. A deployed site needs a separate frontend host/reverse
proxy, HTTPS, and routing for the API and event stream.

## Current tradeoffs and limitations

This is a portfolio implementation, not a production-ready identity system:

- Passwords are compared directly against environment-configured strings.
  There is no password hashing, registration, password reset, or MFA.
- Login has no rate limiter or account lockout.
- Tokens are issued with HS256; verification accepts the HMAC algorithm family
  rather than pinning HS256. Expiry is checked when present, but the parser does
  not explicitly require expiry, issuer, or audience claims.
- There are no refresh tokens or individual token revocation. Logout clears
  browser storage and disconnects the event stream; it does not revoke copies.
- Browser token storage is accessible to same-origin JavaScript.
- SSE tokens appear in URLs and require care in access logs. Existing streams
  are not rechecked when a token expires.
- The Go HTTP listener and local MQTT transport are unencrypted. Production use
  needs HTTPS and appropriate broker authentication, TLS, and topic permissions.
- Never connect a public demonstration to a real pump or production broker.

For this public snapshot, production connection defaults were replaced with
localhost, the development signing-key fallback was removed, and local SSE
proxying was added. The underlying account and JWT model is otherwise retained.
