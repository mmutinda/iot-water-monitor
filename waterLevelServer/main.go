package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/golang-jwt/jwt/v5"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	OK    bool   `json:"ok"`
	Token string `json:"token,omitempty"`
	Role  string `json:"role,omitempty"`
	Error string `json:"error,omitempty"`
}

// Roles. "superadmin" may control the pump and change config; "viewer" is
// read-only (sees levels, analytics, and live pump state, but cannot act).
const (
	roleSuperadmin = "superadmin"
	roleViewer     = "viewer"
)

// account is one login configured from the environment (see loadAccounts).
type account struct {
	username string
	password string
	role     string
}

var accounts []account

// roleCtxKey carries the authenticated user's role from authMiddleware down to
// handlers (so they can enforce per-action authorization).
type ctxKey string

const roleCtxKey ctxKey = "role"

type AnalyticsQuery struct {
	StartTime string `json:"start_time"` // RFC3339 format
	EndTime   string `json:"end_time"`
	Interval  string `json:"interval"` // "raw" or "hourly"
}

type AnalyticsPoint struct {
	Timestamp string  `json:"timestamp"`
	Distance  float64 `json:"distance"`
	AvgDist   float64 `json:"avg_distance,omitempty"` // only for aggregates
	MinDist   float64 `json:"min_distance,omitempty"`
	MaxDist   float64 `json:"max_distance,omitempty"`
	Count     int     `json:"count,omitempty"`
	Tank      string  `json:"tank"`
}

type TankConfig struct {
	Tank           string  `json:"tank"`
	Name           string  `json:"name"`
	TotalHeight    float64 `json:"total_height"`
	SensorOffset   float64 `json:"sensor_offset"`
	MinDistance    float64 `json:"min_distance"`
	MaxDistance    float64 `json:"max_distance"`
	CapacityLiters float64 `json:"capacity_liters"`
}

// MQTTPayload matches the ESP32 publish format: {"distance_cm": 25.1}
type MQTTPayload struct {
	DistanceCM float64 `json:"distance_cm"`
}

// cachedReading holds the most recent reading for one tank, kept in memory.
type cachedReading struct {
	Tank      string  `json:"tank"`
	Distance  float64 `json:"distance"`
	Timestamp string  `json:"timestamp"` // RFC3339
}

var (
	db         *sql.DB
	tzLocation *time.Location
	jwtSecret  []byte
)

// MQTT pump-control topics. The relay ESP32 subscribes to pumpCmdTopic and
// publishes its current state (retained) to pumpStateTopic.
const (
	pumpCmdTopic   = "tank/pump/cmd"   // payload: "on" | "off"
	pumpStateTopic = "tank/pump/state" // payload: "on" | "off" | "offline"
)

// mqttClient is the shared paho client, set up in startMQTTSubscriber and
// reused by the HTTP pump handler to publish commands.
var mqttClient mqtt.Client

// pumpState holds the last known relay state reported on pumpStateTopic.
var (
	pumpMu        sync.RWMutex
	pumpState     = "unknown"
	pumpStateTime string
)

// Overflow interlock: the server refuses to start the pump — and auto-stops it —
// when the destination (roof) tank is at or above pumpFullPct. Seeded from
// PUMP_DEST_TANK / PUMP_FULL_PCT, then overridden by DB-stored values, and
// editable at runtime via PUT /pump/config. An empty dest tank disables it.
//
// Auto-fill: when enabled, the server starts the pump once the destination tank
// drops to autoStartPct and stops it at autoStopPct (hysteresis). It pauses when
// the source tank falls to autoSourceMin so the pump can't run dry unattended.
// While auto-filling, the server sends keep-alive "on" commands so the firmware
// watchdog doesn't stop the fill mid-cycle.
var (
	pumpCfgMu      sync.RWMutex
	pumpDestTank   = "A"
	pumpFullPct    = 90.0
	autoEnabled    = false
	autoStartPct   = 15.0 // dest at/below this → start
	autoStopPct    = 80.0 // dest at/above this → stop
	autoSourceTank = ""   // "" → no dry-run guard
	autoSourceMin  = 15.0 // source at/below this → pause (prevent dry run)
)

// autoConfig holds an immutable snapshot of the auto-fill settings.
type autoConfig struct {
	enabled    bool
	dest       string
	startPct   float64
	stopPct    float64
	sourceTank string
	sourceMin  float64
}

// pumpInterlock returns the current interlock config under a read lock.
func pumpInterlock() (dest string, fullPct float64) {
	pumpCfgMu.RLock()
	defer pumpCfgMu.RUnlock()
	return pumpDestTank, pumpFullPct
}

// setPumpInterlock updates the in-memory interlock config under a write lock.
func setPumpInterlock(dest string, fullPct float64) {
	pumpCfgMu.Lock()
	pumpDestTank, pumpFullPct = dest, fullPct
	pumpCfgMu.Unlock()
}

// getAutoConfig returns a snapshot of the auto-fill config under a read lock.
// The destination tank is the interlock's destination (the tank being filled).
func getAutoConfig() autoConfig {
	pumpCfgMu.RLock()
	defer pumpCfgMu.RUnlock()
	return autoConfig{
		enabled:    autoEnabled,
		dest:       pumpDestTank,
		startPct:   autoStartPct,
		stopPct:    autoStopPct,
		sourceTank: autoSourceTank,
		sourceMin:  autoSourceMin,
	}
}

// setAutoConfig updates the in-memory auto-fill settings under a write lock.
func setAutoConfig(enabled bool, startPct, stopPct float64, sourceTank string, sourceMin float64) {
	pumpCfgMu.Lock()
	autoEnabled, autoStartPct, autoStopPct = enabled, startPct, stopPct
	autoSourceTank, autoSourceMin = sourceTank, sourceMin
	pumpCfgMu.Unlock()
}

// Auto-fill runtime state: autoActive is true while the server is driving the
// pump. A manual pump command suppresses auto (re)starts until autoSuppressedTil
// so the keep-alive loop never fights a human who just pressed Start/Stop.
var (
	autoMu            sync.Mutex
	autoActive        bool
	autoSuppressedTil time.Time
)

const autoManualCooldown = 2 * time.Minute

// manualHoldActive is true while a superadmin holds the pump on from the app or
// website. The server then feeds the relay's 60s safety watchdog on its own 30s
// loop (mirroring auto-fill), so the pump keeps running even when the
// controlling phone locks its screen or the app is closed — the client's own
// keep-alive timers get suspended in that state. The hold clears on a manual
// "off", when the overflow interlock force-stops the pump (destination full), or
// when the controller drops offline.
var (
	manualHoldMu     sync.Mutex
	manualHoldActive bool
)

func setManualHold(active bool) {
	manualHoldMu.Lock()
	manualHoldActive = active
	manualHoldMu.Unlock()
}

func manualHoldIsActive() bool {
	manualHoldMu.Lock()
	defer manualHoldMu.Unlock()
	return manualHoldActive
}

// sseEvent is one server-sent event frame. An empty event name is the default
// (unnamed) message used for tank readings — kept as-is for back-compat with the
// existing client. Named events (e.g. "pump") let the client route by type.
type sseEvent struct {
	event string
	data  []byte
}

// pumpStatePayload is the JSON shape pushed on the "pump" SSE event and returned
// by GET /pump, so both surfaces report identical fields.
type pumpStatePayload struct {
	State     string `json:"state"`
	Timestamp string `json:"timestamp"`
}

var (
	cacheMu     sync.RWMutex
	latestCache = map[string]cachedReading{}

	sseMu      sync.Mutex
	sseClients = map[chan sseEvent]struct{}{}
)

// tankMeta describes a tank loaded from tank_config: a stable id, the
// human-readable name users see, and the MQTT topic carrying its readings.
type tankMeta struct {
	ID    string
	Name  string
	Topic string
}

// The tank registry is loaded from the DB at startup (see loadTanks).
var (
	tanksMu     sync.RWMutex
	tankByID    = map[string]tankMeta{}
	tankByTopic = map[string]string{} // mqtt topic → tank id
	tankList    []string              // tank ids, in display order
)

// notifyState tracks the last alert sent per tank to avoid duplicate messages.
// Values: "high", "low", "normal". Missing keys default to "" (treated as normal).
var notifyState = map[string]string{}

// loadTanks fills the in-memory registry from tank_config. If the name/mqtt_topic
// columns are missing (migration not yet run), it falls back to deriving them
// from the id so the server still starts.
func loadTanks() error {
	rows, err := db.Query(`SELECT tank, name, mqtt_topic FROM tank_config ORDER BY tank`)
	if err != nil {
		log.Printf("[tanks] name/mqtt_topic unavailable (%v) — run migration 0001; deriving defaults", err)
		return loadTanksFallback()
	}
	defer rows.Close()

	byID := map[string]tankMeta{}
	byTopic := map[string]string{}
	var order []string
	for rows.Next() {
		var id string
		var name, topic sql.NullString
		if err := rows.Scan(&id, &name, &topic); err != nil {
			return err
		}
		m := tankMeta{ID: id, Name: strings.TrimSpace(name.String), Topic: strings.TrimSpace(topic.String)}
		if m.Name == "" {
			m.Name = id
		}
		if m.Topic == "" {
			m.Topic = "tank_" + strings.ToLower(id) + "/level"
		}
		byID[id] = m
		byTopic[m.Topic] = id
		order = append(order, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(order) == 0 {
		return fmt.Errorf("no tanks found in tank_config")
	}

	tanksMu.Lock()
	tankByID, tankByTopic, tankList = byID, byTopic, order
	tanksMu.Unlock()
	log.Printf("[tanks] loaded %d tanks: %v", len(order), order)
	return nil
}

// loadTanksFallback derives name/topic from the id for pre-migration databases.
func loadTanksFallback() error {
	rows, err := db.Query(`SELECT tank FROM tank_config ORDER BY tank`)
	if err != nil {
		return err
	}
	defer rows.Close()

	byID := map[string]tankMeta{}
	byTopic := map[string]string{}
	var order []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		topic := "tank_" + strings.ToLower(id) + "/level"
		byID[id] = tankMeta{ID: id, Name: id, Topic: topic}
		byTopic[topic] = id
		order = append(order, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(order) == 0 {
		return fmt.Errorf("no tanks found in tank_config")
	}

	tanksMu.Lock()
	tankByID, tankByTopic, tankList = byID, byTopic, order
	tanksMu.Unlock()
	log.Printf("[tanks] loaded %d tanks (defaults): %v", len(order), order)
	return nil
}

// tankName returns a tank's display name, falling back to its id.
func tankName(id string) string {
	tanksMu.RLock()
	defer tanksMu.RUnlock()
	if m, ok := tankByID[id]; ok && m.Name != "" {
		return m.Name
	}
	return id
}

// tankTopicFilters builds the SubscribeMultiple filter set from configured topics.
func tankTopicFilters() map[string]byte {
	tanksMu.RLock()
	defer tanksMu.RUnlock()
	filters := make(map[string]byte, len(tankByID))
	for _, m := range tankByID {
		filters[m.Topic] = 0
	}
	return filters
}

// tankIDForTopic maps an incoming reading topic back to its tank id.
func tankIDForTopic(topic string) (string, bool) {
	tanksMu.RLock()
	defer tanksMu.RUnlock()
	id, ok := tankByTopic[topic]
	return id, ok
}

func normalizeTank(raw string) (string, error) {
	t := strings.ToUpper(strings.TrimSpace(raw))
	tanksMu.RLock()
	_, ok := tankByID[t]
	tanksMu.RUnlock()
	if !ok {
		return "", fmt.Errorf("invalid tank identifier '%s'", raw)
	}
	return t, nil
}

func main() {
	secret := os.Getenv("JWT_SECRET")
	if strings.TrimSpace(secret) == "" {
		log.Fatal("JWT_SECRET must be set; generate a random signing key before starting the server")
	}
	jwtSecret = []byte(secret)

	var err error
	tzLocation, err = time.LoadLocation("Africa/Nairobi")
	if err != nil {
		log.Fatalf("failed to load timezone: %v", err)
	}
	time.Local = tzLocation
	// Read MySQL config from environment or use defaults
	dbUser := os.Getenv("DB_USER")
	if dbUser == "" {
		dbUser = "root"
	}
	dbPass := os.Getenv("DB_PASS")
	dbHost := os.Getenv("DB_HOST")
	if dbHost == "" {
		dbHost = "localhost:3306"
	}
	dbName := os.Getenv("DB_NAME")
	if dbName == "" {
		dbName = "water_sensor"
	}

	mysqlCfg := mysql.Config{
		User:                 dbUser,
		Passwd:               dbPass,
		Net:                  "tcp",
		Addr:                 dbHost,
		DBName:               dbName,
		Params:               map[string]string{"charset": "utf8mb4", "collation": "utf8mb4_unicode_ci", "multiStatements": "true"},
		ParseTime:            true,
		Loc:                  tzLocation,
		AllowNativePasswords: true,
	}
	db, err = sql.Open("mysql", mysqlCfg.FormatDSN())
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// Test connection
	if err := db.Ping(); err != nil {
		log.Fatalf("failed to ping db: %v", err)
	}

	if _, err := db.Exec("SET time_zone = '+03:00'"); err != nil {
		log.Printf("warning: failed to set time_zone: %v", err)
	}

	fmt.Printf("✓ Connected to MySQL database: %s@%s/%s\n", dbUser, dbHost, dbName)

	loadAccounts()

	// Pump overflow interlock config
	if v := os.Getenv("PUMP_DEST_TANK"); v != "" {
		pumpDestTank = strings.ToUpper(strings.TrimSpace(v))
	}
	if v := os.Getenv("PUMP_FULL_PCT"); v != "" {
		if pct, err := strconv.ParseFloat(v, 64); err == nil {
			pumpFullPct = pct
		} else {
			log.Printf("warning: invalid PUMP_FULL_PCT %q, using %.0f", v, pumpFullPct)
		}
	}
	// DB-stored values win over env/defaults so dashboard edits persist across restarts.
	if v, ok := getSetting("pump_dest_tank"); ok {
		pumpDestTank = strings.ToUpper(strings.TrimSpace(v))
	}
	if v, ok := getSetting("pump_full_pct"); ok {
		if pct, err := strconv.ParseFloat(v, 64); err == nil {
			pumpFullPct = pct
		}
	}
	if pumpDestTank == "" {
		log.Println("[pump] overflow interlock DISABLED (PUMP_DEST_TANK empty)")
	} else {
		log.Printf("[pump] overflow interlock: stop when tank %s >= %.0f%%", pumpDestTank, pumpFullPct)
	}

	// Auto-fill config — persisted in app_settings, edited from the dashboard.
	loadAutoConfig()
	if autoEnabled {
		log.Printf("[pump] auto-fill: fill %s when ≤ %.0f%%, stop at %.0f%% (source %q pauses ≤ %.0f%%)",
			pumpDestTank, autoStartPct, autoStopPct, autoSourceTank, autoSourceMin)
	} else {
		log.Println("[pump] auto-fill DISABLED")
	}
	go autoFillKeepAliveLoop()
	go manualHoldKeepAliveLoop()

	// Load the tank registry (id, display name, MQTT topic) from the DB
	if err := loadTanks(); err != nil {
		log.Fatalf("failed to load tanks: %v", err)
	}
	if err := loadEnvironmentSensors(); err != nil {
		log.Fatalf("failed to load environment sensors (run migration 0002): %v", err)
	}

	// Warm up the in-memory cache with the latest reading per tank
	initLatestCache()
	initEnvironmentCache()

	// Start background aggregation task (every 10 minutes)
	go aggregateHourlyReadings()
	go aggregateEnvironmentHourlyReadings()

	// Start MQTT subscriber (saves readings from ESP32 nodes)
	go startMQTTSubscriber()

	http.HandleFunc("/login", loginHandler)
	http.HandleFunc("/latest", authMiddleware(latestValue))
	http.HandleFunc("/analytics", authMiddleware(analyticsHandler))
	http.HandleFunc("/tanks", authMiddleware(tankConfigHandler))
	http.HandleFunc("/pump", authMiddleware(pumpHandler))
	http.HandleFunc("/pump/config", authMiddleware(pumpConfigHandler))
	http.HandleFunc("/environment/sensors", authMiddleware(environmentSensorsHandler))
	http.HandleFunc("/environment/latest", authMiddleware(environmentLatestHandler))
	http.HandleFunc("/environment/analytics", authMiddleware(environmentAnalyticsHandler))
	http.HandleFunc("/events", sseAuthMiddleware(eventsHandler))

	fmt.Println("Server running on :5001")
	log.Fatal(http.ListenAndServe(":5001", nil))
}

// loginHandler validates credentials and returns {ok: true} or {ok: false, error: "..."}.
func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(loginResponse{OK: false, Error: "invalid request body"})
		return
	}

	w.Header().Set("Content-Type", "application/json")

	for _, a := range accounts {
		// An account with no configured password is disabled; and an empty
		// submitted password never authenticates.
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
				json.NewEncoder(w).Encode(loginResponse{OK: false, Error: "token generation failed"})
				return
			}
			json.NewEncoder(w).Encode(loginResponse{OK: true, Token: tokenStr, Role: a.role})
			return
		}
	}

	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(loginResponse{OK: false, Error: "Invalid username or password"})
}

// loadAccounts builds the login table from the environment. Passwords have NO
// default and are never hardcoded: an account whose password env var is unset
// (empty) is disabled — no credentials can authenticate to it. Set ADMIN_PASS
// and/or VIEWER_PASS to enable each role. Usernames default for convenience.
//
//	ADMIN_USER  (default "admin")    ADMIN_PASS  (required; empty ⇒ disabled)
//	VIEWER_USER (default "viewer")   VIEWER_PASS (required; empty ⇒ disabled)
func loadAccounts() {
	adminUser := getenvDefault("ADMIN_USER", "admin")
	adminPass := os.Getenv("ADMIN_PASS") // no default — empty disables the account
	viewerUser := getenvDefault("VIEWER_USER", "viewer")
	viewerPass := os.Getenv("VIEWER_PASS")

	accounts = []account{
		{username: adminUser, password: adminPass, role: roleSuperadmin},
		{username: viewerUser, password: viewerPass, role: roleViewer},
	}

	if adminPass == "" {
		log.Println("warning: ADMIN_PASS not set — superadmin login is disabled until you set it")
	}
	if viewerPass == "" {
		log.Println("warning: VIEWER_PASS not set — viewer login is disabled until you set it")
	}
	log.Printf("[auth] accounts configured: %s (superadmin), %s (viewer)", adminUser, viewerUser)
}

func getenvDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// authMiddleware validates the Bearer JWT on every protected route and stashes
// the caller's role in the request context for downstream authorization.
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

// roleFromContext returns the authenticated role set by authMiddleware.
func roleFromContext(r *http.Request) string {
	role, _ := r.Context().Value(roleCtxKey).(string)
	return role
}

// requireSuperadmin writes a 403 and returns false unless the caller is a
// superadmin. Tokens issued before roles existed have no role claim and are
// therefore treated as non-privileged.
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

// saveReading persists a validated reading to the database.
func saveReading(tankID string, distance float64) error {
	now := time.Now().In(tzLocation)
	_, err := db.Exec(
		"INSERT INTO readings (tank, distance, created_at) VALUES (?, ?, ?)",
		tankID, distance, now,
	)
	return err
}

// startMQTTSubscriber connects to the broker and saves readings from all tank topics.
// Topics: tank_a/level, tank_b/level, tank_c/level  — payload: {"distance_cm": 25.1}
func startMQTTSubscriber() {
	broker := os.Getenv("MQTT_BROKER")
	if broker == "" {
		broker = "localhost:1883"
	}
	mqttUser := os.Getenv("MQTT_USER")
	mqttPass := os.Getenv("MQTT_PASS")
	hostname, _ := os.Hostname()
	clientID := "water_level_server_" + hostname
	log.Printf("[mqtt] connecting to broker: %s (clientID: %s)", broker, clientID)

	opts := mqtt.NewClientOptions().
		AddBroker("tcp://" + broker).
		SetClientID(clientID).
		SetUsername(mqttUser).
		SetPassword(mqttPass).
		SetCleanSession(true).
		SetAutoReconnect(true).
		SetOnConnectHandler(func(c mqtt.Client) {
			log.Printf("[mqtt] connected to %s", broker)
			filters := tankTopicFilters()
			if token := c.SubscribeMultiple(filters, mqttMessageHandler); token.Wait() && token.Error() != nil {
				log.Printf("[mqtt] subscribe error: %v", token.Error())
			} else {
				topics := make([]string, 0, len(filters))
				for t := range filters {
					topics = append(topics, t)
				}
				log.Printf("[mqtt] subscribed to level topics: %v", topics)
			}
			environmentFilters := environmentTopicFilters()
			if len(environmentFilters) > 0 {
				if token := c.SubscribeMultiple(environmentFilters, environmentMessageHandler); token.Wait() && token.Error() != nil {
					log.Printf("[mqtt] environment subscribe error: %v", token.Error())
				} else {
					log.Printf("[mqtt] subscribed to %d environment topic(s)", len(environmentFilters))
				}
			}
			// Track the relay's reported state so GET /pump can report it.
			if token := c.Subscribe(pumpStateTopic, 0, pumpStateHandler); token.Wait() && token.Error() != nil {
				log.Printf("[mqtt] pump state subscribe error: %v", token.Error())
			} else {
				log.Printf("[mqtt] subscribed to %s", pumpStateTopic)
			}
		}).
		SetConnectionLostHandler(func(c mqtt.Client, err error) {
			log.Printf("[mqtt] connection lost: %v", err)
		})

	mqttClient = mqtt.NewClient(opts)
	if token := mqttClient.Connect(); token.Wait() && token.Error() != nil {
		log.Printf("[mqtt] initial connect failed: %v", token.Error())
	}
}

// pumpStateHandler records the relay's last reported state. "offline" is the
// relay's Last Will, published by the broker when the controller drops off
// ungracefully (power/WiFi loss) — it lets GET /pump distinguish a dead
// controller from a real "off".
func pumpStateHandler(_ mqtt.Client, msg mqtt.Message) {
	state := strings.TrimSpace(string(msg.Payload()))
	if state != "on" && state != "off" && state != "offline" {
		log.Printf("[mqtt] ignoring unexpected pump state: %q", state)
		return
	}
	pumpMu.Lock()
	pumpState = state
	pumpStateTime = time.Now().In(tzLocation).Format(time.RFC3339)
	pumpMu.Unlock()
	// A dead controller can't be kept alive — drop any manual hold so the loop
	// stops trying to publish to it.
	if state == "offline" {
		setManualHold(false)
	}
	log.Printf("[mqtt] pump state = %s", state)
	broadcastPumpState() // push the change to SSE clients (no polling)
}

// mqttMessageHandler is called for every reading message. It maps the topic
// back to its tank id via the registry loaded from tank_config.
func mqttMessageHandler(_ mqtt.Client, msg mqtt.Message) {
	topic := msg.Topic()

	tankID, ok := tankIDForTopic(topic)
	if !ok {
		log.Printf("[mqtt] no tank mapped to topic: %s", topic)
		return
	}

	var p MQTTPayload
	if err := json.Unmarshal(msg.Payload(), &p); err != nil {
		log.Printf("[mqtt] bad payload on %s: %v", topic, err)
		return
	}
	if p.DistanceCM <= 0 {
		log.Printf("[mqtt] invalid distance %.2f on %s — skipping", p.DistanceCM, topic)
		return
	}

	if err := saveReading(tankID, p.DistanceCM); err != nil {
		log.Printf("[mqtt] failed to save tank=%s distance=%.2f: %v", tankID, p.DistanceCM, err)
		return
	}
	log.Printf("[mqtt] saved tank=%s distance=%.2f", tankID, p.DistanceCM)

	reading := cachedReading{
		Tank:      tankID,
		Distance:  p.DistanceCM,
		Timestamp: time.Now().In(tzLocation).Format(time.RFC3339),
	}
	cacheMu.Lock()
	latestCache[tankID] = reading
	cacheMu.Unlock()

	broadcastToSSEClients(reading)
	go checkAndNotify(tankID, p.DistanceCM)
	go maybeAutoStopPump(tankID, p.DistanceCM)
	go evaluateAutoFill(tankID)
}

// GET /latest — returns the most recent reading (served from in-memory cache, DB as fallback)
func latestValue(w http.ResponseWriter, r *http.Request) {
	tankParam := r.URL.Query().Get("tank")
	if tankParam == "" {
		http.Error(w, "missing 'tank' parameter (expected A or B)", http.StatusBadRequest)
		return
	}
	tankID, err := normalizeTank(tankParam)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cacheMu.RLock()
	cached, ok := latestCache[tankID]
	cacheMu.RUnlock()

	if ok {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cached)
		return
	}

	// Cache miss (cold start) — fall back to DB
	var distance float64
	var ts time.Time
	err = db.QueryRow(`
		SELECT distance, created_at FROM readings
		WHERE tank = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, tankID).Scan(&distance, &ts)

	if err == sql.ErrNoRows {
		log.Printf("[latest] no data yet for tank %s", tankID)
		http.Error(w, "no data yet", http.StatusNotFound)
		return
	}
	if err != nil {
		log.Printf("[latest] query error for tank %s: %v", tankID, err)
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cachedReading{
		Tank:      tankID,
		Distance:  distance,
		Timestamp: ts.In(tzLocation).Format(time.RFC3339),
	})
}

// GET /analytics?start_time=2026-02-01T00:00:00Z&end_time=2026-02-11T00:00:00Z&interval=hourly
// interval: "raw" (all readings) or "hourly" (aggregates)
func analyticsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}

	tankParam := r.URL.Query().Get("tank")
	if tankParam == "" {
		http.Error(w, "missing 'tank' parameter (expected A or B)", http.StatusBadRequest)
		return
	}
	tankID, tankError := normalizeTank(tankParam)
	if tankError != nil {
		http.Error(w, tankError.Error(), http.StatusBadRequest)
		return
	}

	startStr := r.URL.Query().Get("start_time")
	endStr := r.URL.Query().Get("end_time")
	interval := r.URL.Query().Get("interval")

	if interval == "" {
		interval = "raw"
	}
	if interval != "raw" && interval != "hourly" {
		http.Error(w, "interval must be 'raw' or 'hourly'", http.StatusBadRequest)
		return
	}

	// Parse times with fallback to last 24 hours
	var startTime, endTime time.Time
	var err error
	now := time.Now().In(tzLocation)

	if startStr == "" {
		startTime = now.Add(-24 * time.Hour)
	} else {
		startTime, err = time.Parse(time.RFC3339, startStr)
		if err != nil {
			http.Error(w, "invalid start_time format (use RFC3339)", http.StatusBadRequest)
			return
		}
		startTime = startTime.In(tzLocation)
	}

	if endStr == "" {
		endTime = now
	} else {
		endTime, err = time.Parse(time.RFC3339, endStr)
		if err != nil {
			http.Error(w, "invalid end_time format (use RFC3339)", http.StatusBadRequest)
			return
		}
		endTime = endTime.In(tzLocation)
	}

	var query string
	var rows *sql.Rows

	if interval == "raw" {
		query = `
			SELECT created_at, distance FROM readings
			WHERE tank = ? AND created_at BETWEEN ? AND ?
			ORDER BY created_at ASC
		`
		rows, err = db.Query(query, tankID, startTime, endTime)
	} else {
		query = `
			SELECT hour_timestamp, avg_distance, min_distance, max_distance, count FROM readings_hourly
			WHERE tank = ? AND hour_timestamp BETWEEN ? AND ?
			ORDER BY hour_timestamp ASC
		`
		rows, err = db.Query(query, tankID, startTime, endTime)
	}

	if err != nil {
		log.Printf("[analytics] query error: %v", err)
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var points []AnalyticsPoint

	if interval == "raw" {
		for rows.Next() {
			var ts time.Time
			var distance float64
			if err := rows.Scan(&ts, &distance); err != nil {
				continue
			}
			points = append(points, AnalyticsPoint{
				Timestamp: ts.In(tzLocation).Format(time.RFC3339),
				Distance:  distance,
				Tank:      tankID,
			})
		}
	} else {
		for rows.Next() {
			var ts time.Time
			var avgDist, minDist, maxDist float64
			var count int
			if err := rows.Scan(&ts, &avgDist, &minDist, &maxDist, &count); err != nil {
				continue
			}
			points = append(points, AnalyticsPoint{
				Timestamp: ts.In(tzLocation).Format(time.RFC3339),
				AvgDist:   avgDist,
				MinDist:   minDist,
				MaxDist:   maxDist,
				Count:     count,
				Tank:      tankID,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"interval":   interval,
		"count":      len(points),
		"start_time": startTime.Format(time.RFC3339),
		"end_time":   endTime.Format(time.RFC3339),
		"tank":       tankID,
		"data":       points,
	})
}

// GET /tanks — returns configuration for all tanks
func tankConfigHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}

	rows, err := db.Query(`
		SELECT tank, total_height, sensor_offset, min_distance, max_distance, capacity_liters
		FROM tank_config
		ORDER BY tank
	`)
	if err != nil {
		log.Printf("[tanks] query error: %v", err)
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	configs := make([]TankConfig, 0, len(tankList))
	for rows.Next() {
		var cfg TankConfig
		if err := rows.Scan(&cfg.Tank, &cfg.TotalHeight, &cfg.SensorOffset, &cfg.MinDistance, &cfg.MaxDistance, &cfg.CapacityLiters); err != nil {
			log.Printf("[tanks] scan error: %v", err)
			continue
		}
		cfg.Name = tankName(cfg.Tank) // from the registry, so /tanks works pre-migration too
		configs = append(configs, cfg)
	}

	if err := rows.Err(); err != nil {
		log.Printf("[tanks] rows error: %v", err)
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count": len(configs),
		"data":  configs,
	})
}

// levelPercent converts a raw sensor distance to a water-level percentage using
// the same formula as the client and checkAndNotify. Result is clamped to 0–100.
func levelPercent(distance, sensorOffset, maxDistance float64) float64 {
	if maxDistance <= 0 {
		return 0
	}
	distanceFromMax := distance - sensorOffset
	waterHeight := maxDistance - distanceFromMax
	pct := (waterHeight / maxDistance) * 100
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct
}

// currentTankPercent returns the latest known fill percentage for a tank.
// ok is false when there is no reading yet or the tank config is missing — the
// caller should treat that as "unknown" and not block on it.
func currentTankPercent(tankID string) (pct float64, ok bool) {
	cacheMu.RLock()
	cached, hit := latestCache[tankID]
	cacheMu.RUnlock()

	distance := 0.0
	if hit {
		distance = cached.Distance
	} else {
		if err := db.QueryRow(`
			SELECT distance FROM readings WHERE tank = ? ORDER BY created_at DESC LIMIT 1
		`, tankID).Scan(&distance); err != nil {
			return 0, false
		}
	}

	var sensorOffset, maxDistance float64
	if err := db.QueryRow(`
		SELECT sensor_offset, max_distance FROM tank_config WHERE tank = ?
	`, tankID).Scan(&sensorOffset, &maxDistance); err != nil || maxDistance <= 0 {
		return 0, false
	}
	return levelPercent(distance, sensorOffset, maxDistance), true
}

// publishPumpCommand sends an on/off command to the relay over MQTT.
func publishPumpCommand(action string) error {
	if mqttClient == nil || !mqttClient.IsConnected() {
		return fmt.Errorf("broker offline")
	}
	token := mqttClient.Publish(pumpCmdTopic, 1, false, action)
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("publish timed out")
	}
	return token.Error()
}

// maybeAutoStopPump force-stops the pump when the destination tank just reported
// full. This is the level-aware half of the overflow safety net (the firmware
// watchdog is the time-based half).
func maybeAutoStopPump(tankID string, distance float64) {
	destTank, fullPct := pumpInterlock()
	if destTank == "" || tankID != destTank {
		return
	}
	pumpMu.RLock()
	running := pumpState == "on"
	pumpMu.RUnlock()
	if !running {
		return
	}

	var sensorOffset, maxDistance float64
	if err := db.QueryRow(`
		SELECT sensor_offset, max_distance FROM tank_config WHERE tank = ?
	`, tankID).Scan(&sensorOffset, &maxDistance); err != nil || maxDistance <= 0 {
		return
	}
	pct := levelPercent(distance, sensorOffset, maxDistance)
	if pct < fullPct {
		return
	}

	// Disengage auto-fill so its keep-alive loop can't restart the pump the
	// interlock just stopped (defensive — normally auto stops below full first).
	suppressAuto()
	// Release any manual hold so its keep-alive loop can't restart the pump
	// either — the destination tank is full.
	setManualHold(false)
	log.Printf("[pump] tank %s at %.1f%% — auto-stopping pump", tankID, pct)
	if err := publishPumpCommand("off"); err != nil {
		log.Printf("[pump] auto-stop publish failed: %v", err)
		return
	}
	go sendTelegramNotification(fmt.Sprintf("🛑 Pump auto-stopped — %s is full (%.0f%%)", tankName(tankID), pct))
}

// evaluateAutoFill is called after each reading. When auto-fill is enabled it
// starts the pump once the destination tank drops to autoStartPct and stops it
// at autoStopPct, pausing whenever the source tank is too low to avoid a dry run.
func evaluateAutoFill(triggerTank string) {
	cfg := getAutoConfig()
	if !cfg.enabled || cfg.dest == "" {
		return
	}
	// Only the destination or source tank can change the decision.
	if triggerTank != cfg.dest && (cfg.sourceTank == "" || triggerTank != cfg.sourceTank) {
		return
	}

	destPct, ok := currentTankPercent(cfg.dest)
	if !ok {
		return // no reading yet — don't act on unknown level
	}
	sourceDry := false
	if cfg.sourceTank != "" {
		if sp, ok := currentTankPercent(cfg.sourceTank); ok && sp <= cfg.sourceMin {
			sourceDry = true
		}
	}

	autoMu.Lock()
	defer autoMu.Unlock()

	if autoActive {
		switch {
		case destPct >= cfg.stopPct:
			autoStopLocked(fmt.Sprintf("%s reached %.0f%% (stop ≥ %.0f%%)", tankName(cfg.dest), destPct, cfg.stopPct))
		case sourceDry:
			autoStopLocked(fmt.Sprintf("source %s low — paused to prevent dry run", tankName(cfg.sourceTank)))
		}
		return
	}

	// Not auto-filling. Start only if the destination is low, the source has
	// water, no manual command is in its cooldown, and the pump isn't already on.
	if destPct > cfg.startPct || sourceDry || time.Now().Before(autoSuppressedTil) {
		return
	}
	pumpMu.RLock()
	st := pumpState
	pumpMu.RUnlock()
	if st == "on" {
		return // already running (manual) — leave it to the operator/interlock
	}
	autoStartLocked(cfg, destPct)
}

// autoStartLocked starts the pump under auto-fill control. Caller holds autoMu.
func autoStartLocked(cfg autoConfig, destPct float64) {
	if err := publishPumpCommand("on"); err != nil {
		log.Printf("[auto] start publish failed: %v", err)
		return
	}
	autoActive = true
	log.Printf("[auto] started — %s at %.1f%% (start ≤ %.0f%%)", tankName(cfg.dest), destPct, cfg.startPct)
	go sendTelegramNotification(fmt.Sprintf("🚰 Auto-fill started — %s at %.0f%%", tankName(cfg.dest), destPct))
}

// autoStopLocked stops auto-fill. Caller holds autoMu.
func autoStopLocked(reason string) {
	autoActive = false
	if err := publishPumpCommand("off"); err != nil {
		log.Printf("[auto] stop publish failed: %v", err)
	}
	log.Printf("[auto] stopped — %s", reason)
	go sendTelegramNotification("🛑 Auto-fill stopped — " + reason)
}

// suppressAuto disengages auto-fill after a manual pump command so the keep-alive
// loop won't fight the operator; auto can re-engage after autoManualCooldown.
func suppressAuto() {
	autoMu.Lock()
	autoActive = false
	autoSuppressedTil = time.Now().Add(autoManualCooldown)
	autoMu.Unlock()
}

// autoFillKeepAliveLoop re-sends "on" every 30s while auto-filling so the relay's
// 60s safety watchdog doesn't stop the fill mid-cycle.
func autoFillKeepAliveLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		autoMu.Lock()
		active := autoActive
		autoMu.Unlock()
		if !active {
			continue
		}
		if err := publishPumpCommand("on"); err != nil {
			log.Printf("[auto] keep-alive publish failed: %v", err)
		}
	}
}

// manualHoldKeepAliveLoop re-sends "on" every 30s while a superadmin is holding
// the pump on from the app/website, so the relay's 60s safety watchdog doesn't
// stop the pump when the controlling phone locks its screen or the app is
// closed. It mirrors autoFillKeepAliveLoop but for manual control. Safety is
// preserved: the overflow interlock (maybeAutoStopPump) force-stops and clears
// the hold when the destination tank is full, and the hold is dropped if the
// controller goes offline. This loop also re-checks the interlock defensively so
// it never restarts the pump against a full tank.
func manualHoldKeepAliveLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if !manualHoldIsActive() {
			continue
		}
		// Controller offline → the pump is already physically off and a publish
		// would be silently dropped (QoS 0, clean session). Release the hold.
		pumpMu.RLock()
		offline := pumpState == "offline"
		pumpMu.RUnlock()
		if offline {
			setManualHold(false)
			log.Printf("[pump] manual hold released — controller offline")
			continue
		}
		// Never re-start the pump against a full destination tank. The interlock
		// normally clears the hold first; this is a defensive second check.
		if destTank, fullPct := pumpInterlock(); destTank != "" {
			if pct, ok := currentTankPercent(destTank); ok && pct >= fullPct {
				setManualHold(false)
				log.Printf("[pump] manual hold released — %s full (%.0f%%)", tankName(destTank), pct)
				continue
			}
		}
		if err := publishPumpCommand("on"); err != nil {
			log.Printf("[pump] manual keep-alive publish failed: %v", err)
		}
	}
}

// loadAutoConfig seeds the auto-fill settings from app_settings (DB), keeping the
// in-memory defaults for any key that is absent. Called once at startup.
func loadAutoConfig() {
	if v, ok := getSetting("pump_auto_enabled"); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			autoEnabled = b
		}
	}
	if v, ok := getSetting("pump_auto_start_pct"); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			autoStartPct = f
		}
	}
	if v, ok := getSetting("pump_auto_stop_pct"); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			autoStopPct = f
		}
	}
	if v, ok := getSetting("pump_auto_source_tank"); ok {
		autoSourceTank = strings.ToUpper(strings.TrimSpace(v))
	}
	if v, ok := getSetting("pump_auto_source_min_pct"); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			autoSourceMin = f
		}
	}
}

// getSetting reads a value from app_settings; ok is false when the key is absent.
func getSetting(key string) (string, bool) {
	var v string
	if err := db.QueryRow("SELECT setting_value FROM app_settings WHERE setting_key = ?", key).Scan(&v); err != nil {
		return "", false
	}
	return v, true
}

// saveSetting upserts a key/value into app_settings.
func saveSetting(key, value string) error {
	_, err := db.Exec(`
		INSERT INTO app_settings (setting_key, setting_value) VALUES (?, ?)
		ON DUPLICATE KEY UPDATE setting_value = VALUES(setting_value)
	`, key, value)
	return err
}

// pumpConfigRequest uses pointers so callers can update one field without
// clobbering the other (omitted fields keep their current value).
type pumpConfigRequest struct {
	DestTank *string  `json:"dest_tank"` // "A"|"B"|"C" or "" / "off" to disable
	FullPct  *float64 `json:"full_pct"`  // 0–100
	// Auto-fill (all optional; omitted fields keep their current value).
	AutoEnabled    *bool    `json:"auto_enabled"`
	AutoStartPct   *float64 `json:"auto_start_pct"`   // dest at/below → start
	AutoStopPct    *float64 `json:"auto_stop_pct"`    // dest at/above → stop
	AutoSourceTank *string  `json:"auto_source_tank"` // "" disables the dry-run guard
	AutoSourceMin  *float64 `json:"auto_source_min_pct"`
}

// pumpConfigResponse is the shared GET/PUT body for /pump/config.
func pumpConfigResponse() map[string]interface{} {
	dest, full := pumpInterlock()
	a := getAutoConfig()
	return map[string]interface{}{
		"dest_tank":           dest,
		"full_pct":            full,
		"enabled":             dest != "",
		"auto_enabled":        a.enabled,
		"auto_start_pct":      a.startPct,
		"auto_stop_pct":       a.stopPct,
		"auto_source_tank":    a.sourceTank,
		"auto_source_min_pct": a.sourceMin,
	}
}

// pumpConfigHandler reads and updates the overflow-interlock config.
//
//	GET /pump/config  — current { dest_tank, full_pct, enabled }
//	PUT /pump/config  — update either field; persists to app_settings
func pumpConfigHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		json.NewEncoder(w).Encode(pumpConfigResponse())

	case http.MethodPut:
		// Changing pump config is a superadmin-only action.
		if !requireSuperadmin(w, r) {
			return
		}
		var req pumpConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
			return
		}

		dest, pct := pumpInterlock() // interlock fields
		a := getAutoConfig()         // auto-fill fields
		badRequest := func(msg string) {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": msg})
		}

		if req.DestTank != nil {
			d := strings.ToUpper(strings.TrimSpace(*req.DestTank))
			switch d {
			case "", "OFF", "NONE", "DISABLED":
				d = "" // disable interlock
			default:
				if _, err := normalizeTank(d); err != nil {
					badRequest(err.Error())
					return
				}
			}
			dest = d
		}
		if req.FullPct != nil {
			if *req.FullPct < 0 || *req.FullPct > 100 {
				badRequest("full_pct must be between 0 and 100")
				return
			}
			pct = *req.FullPct
		}

		// Auto-fill fields.
		if req.AutoEnabled != nil {
			a.enabled = *req.AutoEnabled
		}
		if req.AutoStartPct != nil {
			if *req.AutoStartPct < 0 || *req.AutoStartPct > 100 {
				badRequest("auto_start_pct must be between 0 and 100")
				return
			}
			a.startPct = *req.AutoStartPct
		}
		if req.AutoStopPct != nil {
			if *req.AutoStopPct < 0 || *req.AutoStopPct > 100 {
				badRequest("auto_stop_pct must be between 0 and 100")
				return
			}
			a.stopPct = *req.AutoStopPct
		}
		if req.AutoSourceTank != nil {
			s := strings.ToUpper(strings.TrimSpace(*req.AutoSourceTank))
			switch s {
			case "", "OFF", "NONE", "DISABLED":
				s = "" // disable dry-run guard
			default:
				if _, err := normalizeTank(s); err != nil {
					badRequest(err.Error())
					return
				}
			}
			a.sourceTank = s
		}
		if req.AutoSourceMin != nil {
			if *req.AutoSourceMin < 0 || *req.AutoSourceMin > 100 {
				badRequest("auto_source_min_pct must be between 0 and 100")
				return
			}
			a.sourceMin = *req.AutoSourceMin
		}
		if a.startPct >= a.stopPct {
			badRequest("auto_start_pct must be below auto_stop_pct")
			return
		}
		if a.enabled && dest == "" {
			badRequest("set a destination tank before enabling auto-fill")
			return
		}

		settings := map[string]string{
			"pump_dest_tank":           dest,
			"pump_full_pct":            strconv.FormatFloat(pct, 'f', -1, 64),
			"pump_auto_enabled":        strconv.FormatBool(a.enabled),
			"pump_auto_start_pct":      strconv.FormatFloat(a.startPct, 'f', -1, 64),
			"pump_auto_stop_pct":       strconv.FormatFloat(a.stopPct, 'f', -1, 64),
			"pump_auto_source_tank":    a.sourceTank,
			"pump_auto_source_min_pct": strconv.FormatFloat(a.sourceMin, 'f', -1, 64),
		}
		for k, v := range settings {
			if err := saveSetting(k, v); err != nil {
				log.Printf("[pump] save %s failed: %v", k, err)
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "failed to save setting"})
				return
			}
		}

		setPumpInterlock(dest, pct)
		setAutoConfig(a.enabled, a.startPct, a.stopPct, a.sourceTank, a.sourceMin)
		// If auto-fill was just turned off, drop any active auto session.
		if !a.enabled {
			autoMu.Lock()
			autoActive = false
			autoMu.Unlock()
		}
		log.Printf("[pump] config updated: dest=%q full=%.0f%% auto=%t start=%.0f stop=%.0f src=%q srcMin=%.0f",
			dest, pct, a.enabled, a.startPct, a.stopPct, a.sourceTank, a.sourceMin)

		json.NewEncoder(w).Encode(pumpConfigResponse())

	default:
		http.Error(w, "GET or PUT only", http.StatusMethodNotAllowed)
	}
}

type pumpRequest struct {
	Action string `json:"action"` // "on" | "off"
}

// pumpHandler controls the pump relay over MQTT so it works from anywhere.
//
//	POST /pump  {"action":"on"|"off"}  — publishes the command to the relay
//	GET  /pump                         — returns the last reported relay state
//
// The "state" field is the relay's reported *switch* position ("on"/"off"), or
// "offline" from its Last Will — not a measurement of actual water flow. Without
// current/flow sensing, a welded contact or jammed pump can't be detected here.
func pumpHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodGet {
		pumpMu.RLock()
		payload := pumpStatePayload{State: pumpState, Timestamp: pumpStateTime}
		pumpMu.RUnlock()
		json.NewEncoder(w).Encode(payload)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "GET or POST only", http.StatusMethodNotAllowed)
		return
	}

	// Controlling the pump is a superadmin-only action (viewers are read-only).
	if !requireSuperadmin(w, r) {
		return
	}

	var req pumpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action != "on" && action != "off" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "action must be 'on' or 'off'"})
		return
	}

	// Don't claim success for a command the relay can't receive. The relay
	// subscribes at QoS 0 with a clean session, so the broker won't queue
	// commands while it's offline — a publish would be silently dropped and the
	// caller misled into thinking the pump acted. The relay's Last Will reports
	// "offline" when it drops off, so reject up front. (A live pump that loses
	// power is already physically off, so refusing "off" loses nothing.)
	pumpMu.RLock()
	controllerState := pumpState
	pumpMu.RUnlock()
	if controllerState == "offline" {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "pump controller is offline — command not delivered",
			"code":  "controller_offline",
		})
		return
	}

	// Overflow interlock — refuse to start the pump when the destination tank is
	// already full. "off" is always allowed (you can always stop the pump).
	if action == "on" {
		if destTank, fullPct := pumpInterlock(); destTank != "" {
			if pct, ok := currentTankPercent(destTank); ok && pct >= fullPct {
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error":     fmt.Sprintf("%s is full (%.0f%%) — refusing to start pump", tankName(destTank), pct),
					"code":      "tank_full",
					"tank":      destTank,
					"level_pct": pct,
				})
				return
			}
		}
	}

	if err := publishPumpCommand(action); err != nil {
		log.Printf("[pump] publish error: %v", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "pump controller unavailable: " + err.Error()})
		return
	}
	// A manual command takes precedence — disengage auto-fill so its keep-alive
	// loop doesn't fight the operator (re-engages after a short cooldown).
	suppressAuto()
	// Hold the pump on (or release it) server-side so it keeps running even if
	// the controlling phone locks its screen — the client keep-alive can't be
	// relied on then. Cleared by "off", the interlock, or a controller dropout.
	if action == "on" {
		setManualHold(true)
	} else {
		setManualHold(false)
	}
	log.Printf("[pump] command sent: %s", action)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":     true,
		"action": action,
	})
}

// initLatestCache pre-fills the in-memory cache with the most recent DB reading per tank.
func initLatestCache() {
	for _, tankID := range tankList {
		var distance float64
		var ts time.Time
		err := db.QueryRow(`
			SELECT distance, created_at FROM readings
			WHERE tank = ?
			ORDER BY created_at DESC
			LIMIT 1
		`, tankID).Scan(&distance, &ts)
		if err != nil {
			continue
		}
		cacheMu.Lock()
		latestCache[tankID] = cachedReading{
			Tank:      tankID,
			Distance:  distance,
			Timestamp: ts.In(tzLocation).Format(time.RFC3339),
		}
		cacheMu.Unlock()
	}
	log.Println("[cache] latest readings loaded")
}

// broadcastSSE fans one event out to every connected SSE client, skipping any
// whose buffer is full (slow/blocked clients are dropped, not waited on).
func broadcastSSE(ev sseEvent) {
	sseMu.Lock()
	defer sseMu.Unlock()
	for ch := range sseClients {
		select {
		case ch <- ev:
		default: // skip slow/blocked clients
		}
	}
}

// broadcastToSSEClients pushes a tank reading as the default (unnamed) event.
func broadcastToSSEClients(r cachedReading) {
	b, _ := json.Marshal(r)
	broadcastSSE(sseEvent{data: b})
}

// broadcastPumpState pushes the current relay state on the "pump" event so the
// UI reflects it live instead of polling.
func broadcastPumpState() {
	pumpMu.RLock()
	b, _ := json.Marshal(pumpStatePayload{State: pumpState, Timestamp: pumpStateTime})
	pumpMu.RUnlock()
	broadcastSSE(sseEvent{event: "pump", data: b})
}

// eventsHandler is an SSE endpoint that streams live tank readings and pump
// state to the browser. On connect it sends a snapshot of all tanks plus the
// current pump state, then pushes each update as it arrives — so the client
// never needs to poll. Tank readings use the default event; pump state uses the
// named "pump" event.
func eventsHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	writeFrame := func(ev sseEvent) {
		if ev.event != "" {
			fmt.Fprintf(w, "event: %s\n", ev.event)
		}
		fmt.Fprintf(w, "data: %s\n\n", ev.data)
	}

	ch := make(chan sseEvent, 16)
	sseMu.Lock()
	sseClients[ch] = struct{}{}
	sseMu.Unlock()

	defer func() {
		sseMu.Lock()
		delete(sseClients, ch)
		sseMu.Unlock()
		close(ch)
	}()

	// Snapshot on connect: current state of all tanks, then the pump state.
	cacheMu.RLock()
	for _, reading := range latestCache {
		b, _ := json.Marshal(reading)
		writeFrame(sseEvent{data: b})
	}
	cacheMu.RUnlock()
	pumpMu.RLock()
	pb, _ := json.Marshal(pumpStatePayload{State: pumpState, Timestamp: pumpStateTime})
	pumpMu.RUnlock()
	writeFrame(sseEvent{event: "pump", data: pb})
	environmentCacheMu.RLock()
	for _, reading := range latestEnvironmentCache {
		b, _ := json.Marshal(reading)
		writeFrame(sseEvent{event: "environment", data: b})
	}
	environmentCacheMu.RUnlock()
	flusher.Flush()

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return
			}
			writeFrame(ev)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// sseAuthMiddleware validates a JWT passed as a ?token= query param (EventSource
// does not support custom headers, so the Bearer header approach cannot be used).
func sseAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokenStr := r.URL.Query().Get("token")
		if tokenStr == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
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
		next(w, r)
	}
}

// sendTelegramNotification posts a message to the configured Telegram bot.
func sendTelegramNotification(message string) {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		log.Printf("[Missing] bot token")
		return
	}
	chatID := os.Getenv("TELEGRAM_CHAT_ID")
	if chatID == "" {
		log.Printf("[Missing] chat id")
		return
	}
	apiURL := fmt.Sprintf(
		"https://api.telegram.org/bot%s/sendMessage?chat_id=%s&text=%s",
		botToken, chatID, url.QueryEscape(message),
	)
	resp, err := http.Get(apiURL)
	if err != nil {
		log.Printf("[telegram] send error: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("[telegram] unexpected status: %d", resp.StatusCode)
	}
}

// checkAndNotify calculates the water level percentage for the given tank and
// sends a Telegram alert when the level crosses 90% (high) or 15% (low).
// Notifications are edge-triggered — only sent once per threshold crossing.
func checkAndNotify(tankID string, distance float64) {
	var sensorOffset, maxDistance float64
	err := db.QueryRow(`
		SELECT sensor_offset, max_distance FROM tank_config WHERE tank = ?
	`, tankID).Scan(&sensorOffset, &maxDistance)
	if err != nil {
		log.Printf("[notify] failed to load config for tank %s: %v", tankID, err)
		return
	}
	if maxDistance <= 0 {
		return
	}

	pct := levelPercent(distance, sensorOffset, maxDistance)

	name := tankName(tankID)
	prev := notifyState[tankID]
	switch {
	case pct >= 90 && prev != "high":
		notifyState[tankID] = "high"
		msg := fmt.Sprintf("⚠️ %s is FULL — level at %.1f%%", name, pct)
		log.Printf("[notify] %s", msg)
		go sendTelegramNotification(msg)
	case pct <= 15 && prev != "low":
		notifyState[tankID] = "low"
		msg := fmt.Sprintf("🚨 %s is LOW — level at %.1f%%", name, pct)
		log.Printf("[notify] %s", msg)
		go sendTelegramNotification(msg)
	case pct > 15 && pct < 90:
		notifyState[tankID] = "normal"
	}
}

// aggregateHourlyReadings runs every 10 minutes to compute hourly stats
func aggregateHourlyReadings() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		windowEnd := time.Now().In(tzLocation).Truncate(time.Hour)
		for _, tankID := range tankList {
			if err := aggregateTankHours(tankID, windowEnd); err != nil {
				log.Printf("[aggregator] tank %s: %v", tankID, err)
			}
		}
	}
}

func aggregateTankHours(tankID string, windowEnd time.Time) error {
	if windowEnd.IsZero() {
		return nil
	}

	var lastAggregated sql.NullTime
	if err := db.QueryRow(`
		SELECT MAX(hour_timestamp)
		FROM readings_hourly
		WHERE tank = ?
	`, tankID).Scan(&lastAggregated); err != nil {
		return fmt.Errorf("query last aggregate: %w", err)
	}

	var startHour time.Time
	if lastAggregated.Valid {
		startHour = lastAggregated.Time.In(tzLocation).Add(time.Hour).Truncate(time.Hour)
	} else {
		var firstReading sql.NullTime
		if err := db.QueryRow(`
			SELECT MIN(created_at)
			FROM readings
			WHERE tank = ?
		`, tankID).Scan(&firstReading); err != nil {
			return fmt.Errorf("query first reading: %w", err)
		}
		if !firstReading.Valid {
			return nil
		}
		startHour = firstReading.Time.In(tzLocation).Truncate(time.Hour)
	}

	if !startHour.Before(windowEnd) {
		return nil
	}

	rows, err := db.Query(`
		SELECT
			DATE_FORMAT(created_at, '%Y-%m-%d %H:00:00') AS hour_slot,
			AVG(distance) AS avg_dist,
			MIN(distance) AS min_dist,
			MAX(distance) AS max_dist,
			COUNT(*) AS cnt
		FROM readings
		WHERE tank = ? AND created_at >= ? AND created_at < ?
		GROUP BY DATE_FORMAT(created_at, '%Y-%m-%d %H:00:00')
		ORDER BY hour_slot
	`, tankID, startHour, windowEnd)
	if err != nil {
		return fmt.Errorf("query aggregates: %w", err)
	}
	defer rows.Close()

	stmt, err := db.Prepare(`
		INSERT INTO readings_hourly (tank, hour_timestamp, avg_distance, min_distance, max_distance, count)
		VALUES (?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			avg_distance = VALUES(avg_distance),
			min_distance = VALUES(min_distance),
			max_distance = VALUES(max_distance),
			count = VALUES(count)
	`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	for rows.Next() {
		var hourSlot string
		var avgDist, minDist, maxDist float64
		var cnt int

		if err := rows.Scan(&hourSlot, &avgDist, &minDist, &maxDist, &cnt); err != nil {
			return fmt.Errorf("scan aggregate row: %w", err)
		}

		bucketTime, err := time.ParseInLocation("2006-01-02 15:04:05", hourSlot, tzLocation)
		if err != nil {
			return fmt.Errorf("parse hour slot %q: %w", hourSlot, err)
		}

		if _, err := stmt.Exec(tankID, bucketTime, avgDist, minDist, maxDist, cnt); err != nil {
			return fmt.Errorf("insert aggregate: %w", err)
		}
		log.Printf("[aggregator] tank %s hour %s avg=%.2f", tankID, bucketTime.Format(time.RFC3339), avgDist)
	}

	return rows.Err()
}
