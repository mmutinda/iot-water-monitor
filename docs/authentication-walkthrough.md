# Authentication Code Walkthrough

Authentication code in `waterLevelServer` and `waterLevelClient`.
Snippets omit unrelated surrounding code.

## 1. Server: create the JWT after checking credentials

**File:** `waterLevelServer/main.go` - `loginHandler()`

```go
for _, a := range accounts {
    if a.password == "" {
        continue
    }
    if req.Username == a.username && req.Password == a.password {
        claims := jwt.MapClaims{
            "sub":  a.username,
            "role": a.role,
            "iat":  time.Now().Unix(),
            "exp":  time.Now().Add(24 * time.Hour).Unix(),
        }
        token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
        tokenStr, err := token.SignedString(jwtSecret)
        if err != nil {
            w.WriteHeader(http.StatusInternalServerError)
            json.NewEncoder(w).Encode(loginResponse{
                OK: false, Error: "token generation failed",
            })
            return
        }
        json.NewEncoder(w).Encode(loginResponse{
            OK: true, Token: tokenStr, Role: a.role,
        })
        return
    }
}
```

The server checks the submitted credentials against its configured accounts.
`NewWithClaims()` builds a JWT containing the username, role, issued-at time,
and 24-hour expiration. `SignedString(jwtSecret)` signs it with HS256 and
returns the token string sent to the client.

## 2. Client: receive and store the JWT

**File:** `waterLevelClient/src/components/LoginPage.jsx` - `handleSubmit()`

```jsx
const res = await fetch('/login', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ username, password }),
});
const data = await res.json();
if (data.ok && data.token) {
  setToken(data.token);
  setRole(data.role);
  onLogin();
}
```

The client posts credentials, saves the returned token and role, and switches
to the dashboard. It does not create or sign a token.

**File:** `waterLevelClient/src/utils/auth.js` - storage helpers

```js
const TOKEN_KEY = "token";
const ROLE_KEY = "role";

export function getToken() {
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(token) {
  localStorage.setItem(TOKEN_KEY, token);
}

export function setRole(role) {
  if (role) localStorage.setItem(ROLE_KEY, role);
  else localStorage.removeItem(ROLE_KEY);
}
```

The token persists in `localStorage`. The separately stored role controls UI
visibility; backend permissions depend on the signed role inside the JWT.

## 3. Client: attach the JWT to API requests

**File:** `waterLevelClient/src/utils/auth.js` - `authFetch()`

```js
export async function authFetch(url, options = {}) {
  const token = getToken();
  const headers = {
    ...(options.headers || {}),
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  };
  const res = await fetch(url, { ...options, headers });
  if (res.status === 401) {
    clearAuth();
    window.location.reload();
  }
  return res;
}
```

`authFetch()` reads the saved JWT and adds the bearer authorization header.
HTTP 401 clears local authentication and reloads the login screen.

**File:** `waterLevelClient/src/hooks/useWaterLevel.js` - example call

```js
const API_URL = '/latest';

authFetch(`${API_URL}?tank=${encodeURIComponent(tank)}`)
  .then((r) => {
    if (!r.ok) throw new Error(`HTTP ${r.status}`);
    return r.json();
  })
```

The hook requests protected data without resending the password or knowing
the server's signing secret.

## 4. Server: validate the JWT before running the handler

**File:** `waterLevelServer/main.go` - route registration

```go
http.HandleFunc("/latest", authMiddleware(latestValue))
http.HandleFunc("/pump", authMiddleware(pumpHandler))
http.HandleFunc("/pump/config", authMiddleware(pumpConfigHandler))
```

Wrapping a handler with `authMiddleware()` makes validation run first.

**File:** `waterLevelServer/main.go` - `authMiddleware()`

```go
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        authHeader := r.Header.Get("Authorization")
        if !strings.HasPrefix(authHeader, "Bearer ") {
            http.Error(w, "unauthorized", http.StatusUnauthorized)
            return
        }
        tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
        token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
            if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
                return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
            }
            return jwtSecret, nil
        })
        if err != nil || !token.Valid {
            http.Error(w, "unauthorized", http.StatusUnauthorized)
            return
        }
        role := ""
        if claims, ok := token.Claims.(jwt.MapClaims); ok {
            role, _ = claims["role"].(string)
        }
        next(w, r.WithContext(context.WithValue(r.Context(), roleCtxKey, role)))
    }
}
```

The middleware extracts the bearer token and rejects non-HMAC signing methods.
`jwt.Parse()` verifies the signature with `jwtSecret` and validates claims,
including expiration when present. Invalid tokens receive HTTP 401.
Only after validation does the middleware pass the verified role through the
request context and call the actual handler.

## 5. Server: authorize privileged actions using the verified role

**File:** `waterLevelServer/main.go` - `requireSuperadmin()`

```go
func requireSuperadmin(w http.ResponseWriter, r *http.Request) bool {
    if roleFromContext(r) != roleSuperadmin {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusForbidden)
        json.NewEncoder(w).Encode(map[string]string{
            "error": "superadmin role required",
            "code":  "forbidden",
        })
        return false
    }
    return true
}
```

`pumpHandler()` calls this before accepting a POST action.
`pumpConfigHandler()` calls it in its PUT branch:

```go
if !requireSuperadmin(w, r) {
    return
}
```

A valid token authenticates the caller. This additional role check authorizes
the action: viewers receive HTTP 403; superadmins may proceed.

## 6. Client and server: authenticate the live event stream

**File:** `waterLevelClient/src/utils/sseClient.js` - `connect()`

```js
const token = getToken();
if (!token) return;

eventSource = new EventSource(`/events?token=${encodeURIComponent(token)}`);
```

Browser `EventSource` cannot attach arbitrary headers, so the client puts the
JWT in a query parameter instead.

**File:** `waterLevelServer/main.go` - route registration

```go
http.HandleFunc("/events", sseAuthMiddleware(eventsHandler))
```

`sseAuthMiddleware()` extracts the token from the URL:

```go
tokenStr := r.URL.Query().Get("token")
if tokenStr == "" {
    http.Error(w, "unauthorized", http.StatusUnauthorized)
    return
}
```

It then uses `jwt.Parse()` with the same HMAC-method check, signing secret,
and invalid-token rejection as the normal API middleware. A valid token
allows `next(w, r)` to start the stream. Validation happens at connection time,
not continuously during an open stream.

## End-to-end trace

```text
LoginPage.handleSubmit()
  -> POST /login with username and password
  -> loginHandler(): compare configured credentials
  -> NewWithClaims() + SignedString(): issue JWT
  -> setToken(): save JWT in localStorage
  -> useWaterLevel() calls authFetch()
  -> authFetch(): attach bearer authorization header
  -> authMiddleware(): validate JWT and extract role
  -> latestValue(): return protected data

For privileged pump actions:
  -> authMiddleware(): authenticate token
  -> pumpHandler() or pumpConfigHandler()
  -> requireSuperadmin(): authorize using verified role
```
