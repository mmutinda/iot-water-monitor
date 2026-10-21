package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type environmentSensor struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	MQTTTopic       string   `json:"mqtt_topic"`
	TemperatureLow  *float64 `json:"temperature_low"`
	TemperatureHigh *float64 `json:"temperature_high"`
	HumidityLow     *float64 `json:"humidity_low"`
	HumidityHigh    *float64 `json:"humidity_high"`
}

type environmentPayload struct {
	TemperatureC *float64 `json:"temperature_c"`
	HumidityPct  *float64 `json:"humidity_pct"`
}

type environmentReading struct {
	SensorID    string  `json:"sensor_id"`
	SensorName  string  `json:"sensor_name"`
	Temperature float64 `json:"temperature_c"`
	Humidity    float64 `json:"humidity_pct"`
	Timestamp   string  `json:"timestamp"`
}

var (
	environmentSensorsMu     sync.RWMutex
	environmentSensorsByID   = map[string]environmentSensor{}
	environmentSensorByTopic = map[string]string{}
	environmentSensorOrder   []string
	environmentCacheMu       sync.RWMutex
	latestEnvironmentCache   = map[string]environmentReading{}
	environmentNotifyStateMu sync.Mutex
	environmentNotifyState   = map[string]string{}
)

func nullableFloat(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	value := v.Float64
	return &value
}

func loadEnvironmentSensors() error {
	rows, err := db.Query(`
		SELECT id, name, mqtt_topic,
		       temperature_low, temperature_high, humidity_low, humidity_high
		FROM environment_sensors
		ORDER BY name, id
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	byID := make(map[string]environmentSensor)
	byTopic := make(map[string]string)
	var order []string
	for rows.Next() {
		var sensor environmentSensor
		var tempLow, tempHigh, humidityLow, humidityHigh sql.NullFloat64
		if err := rows.Scan(
			&sensor.ID, &sensor.Name, &sensor.MQTTTopic,
			&tempLow, &tempHigh, &humidityLow, &humidityHigh,
		); err != nil {
			return err
		}
		sensor.TemperatureLow = nullableFloat(tempLow)
		sensor.TemperatureHigh = nullableFloat(tempHigh)
		sensor.HumidityLow = nullableFloat(humidityLow)
		sensor.HumidityHigh = nullableFloat(humidityHigh)
		byID[sensor.ID] = sensor
		byTopic[sensor.MQTTTopic] = sensor.ID
		order = append(order, sensor.ID)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	environmentSensorsMu.Lock()
	environmentSensorsByID = byID
	environmentSensorByTopic = byTopic
	environmentSensorOrder = order
	environmentSensorsMu.Unlock()
	log.Printf("[environment] loaded %d sensor(s)", len(order))
	return nil
}

func environmentTopicFilters() map[string]byte {
	environmentSensorsMu.RLock()
	defer environmentSensorsMu.RUnlock()
	filters := make(map[string]byte, len(environmentSensorByTopic))
	for topic := range environmentSensorByTopic {
		filters[topic] = 0
	}
	return filters
}

func environmentMessageHandler(_ mqtt.Client, msg mqtt.Message) {
	environmentSensorsMu.RLock()
	sensorID, ok := environmentSensorByTopic[msg.Topic()]
	sensor := environmentSensorsByID[sensorID]
	environmentSensorsMu.RUnlock()
	if !ok {
		log.Printf("[environment] no sensor mapped to topic %s", msg.Topic())
		return
	}

	var payload environmentPayload
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		log.Printf("[environment] invalid payload on %s: %v", msg.Topic(), err)
		return
	}
	if payload.TemperatureC == nil || payload.HumidityPct == nil {
		log.Printf("[environment] missing temperature_c or humidity_pct on %s", msg.Topic())
		return
	}
	temperature := *payload.TemperatureC
	humidity := *payload.HumidityPct
	if math.IsNaN(temperature) || math.IsInf(temperature, 0) ||
		math.IsNaN(humidity) || math.IsInf(humidity, 0) ||
		temperature < -40 || temperature > 85 ||
		humidity < 0 || humidity > 100 {
		log.Printf("[environment] out-of-range reading on %s: temperature=%.2f humidity=%.2f",
			msg.Topic(), temperature, humidity)
		return
	}

	now := time.Now().In(tzLocation)
	if _, err := db.Exec(`
		INSERT INTO environment_readings
			(sensor_id, temperature_c, humidity_pct, created_at)
		VALUES (?, ?, ?, ?)
	`, sensorID, temperature, humidity, now); err != nil {
		log.Printf("[environment] failed to save sensor %s: %v", sensorID, err)
		return
	}

	reading := environmentReading{
		SensorID:    sensorID,
		SensorName:  sensor.Name,
		Temperature: temperature,
		Humidity:    humidity,
		Timestamp:   now.Format(time.RFC3339),
	}
	environmentCacheMu.Lock()
	latestEnvironmentCache[sensorID] = reading
	environmentCacheMu.Unlock()

	data, _ := json.Marshal(reading)
	broadcastSSE(sseEvent{event: "environment", data: data})
	go checkEnvironmentAlerts(sensor, reading)
	log.Printf("[environment] saved sensor=%s temperature=%.2f humidity=%.2f",
		sensorID, temperature, humidity)
}

func initEnvironmentCache() {
	environmentSensorsMu.RLock()
	sensorIDs := append([]string(nil), environmentSensorOrder...)
	sensors := make(map[string]environmentSensor, len(environmentSensorsByID))
	for id, sensor := range environmentSensorsByID {
		sensors[id] = sensor
	}
	environmentSensorsMu.RUnlock()

	for _, sensorID := range sensorIDs {
		var temperature, humidity float64
		var timestamp time.Time
		err := db.QueryRow(`
			SELECT temperature_c, humidity_pct, created_at
			FROM environment_readings
			WHERE sensor_id = ?
			ORDER BY created_at DESC
			LIMIT 1
		`, sensorID).Scan(&temperature, &humidity, &timestamp)
		if err != nil {
			continue
		}
		environmentCacheMu.Lock()
		latestEnvironmentCache[sensorID] = environmentReading{
			SensorID:    sensorID,
			SensorName:  sensors[sensorID].Name,
			Temperature: temperature,
			Humidity:    humidity,
			Timestamp:   timestamp.In(tzLocation).Format(time.RFC3339),
		}
		environmentCacheMu.Unlock()
	}
}

func environmentSensorsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	environmentSensorsMu.RLock()
	sensors := make([]environmentSensor, 0, len(environmentSensorOrder))
	for _, id := range environmentSensorOrder {
		sensors = append(sensors, environmentSensorsByID[id])
	}
	environmentSensorsMu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"count": len(sensors), "data": sensors})
}

func environmentLatestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}

	sensorID := strings.TrimSpace(r.URL.Query().Get("sensor"))
	environmentCacheMu.RLock()
	defer environmentCacheMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if sensorID != "" {
		reading, ok := latestEnvironmentCache[sensorID]
		if !ok {
			http.Error(w, "no data for sensor", http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(reading)
		return
	}

	readings := make([]environmentReading, 0, len(latestEnvironmentCache))
	for _, reading := range latestEnvironmentCache {
		readings = append(readings, reading)
	}
	sort.Slice(readings, func(i, j int) bool { return readings[i].SensorName < readings[j].SensorName })
	json.NewEncoder(w).Encode(map[string]interface{}{"count": len(readings), "data": readings})
}

func environmentAnalyticsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	sensorID := strings.TrimSpace(r.URL.Query().Get("sensor"))
	environmentSensorsMu.RLock()
	_, sensorExists := environmentSensorsByID[sensorID]
	environmentSensorsMu.RUnlock()
	if !sensorExists {
		http.Error(w, "invalid or missing sensor", http.StatusBadRequest)
		return
	}

	interval := r.URL.Query().Get("interval")
	if interval == "" {
		interval = "raw"
	}
	if interval != "raw" && interval != "hourly" {
		http.Error(w, "interval must be 'raw' or 'hourly'", http.StatusBadRequest)
		return
	}

	now := time.Now().In(tzLocation)
	startTime := now.Add(-24 * time.Hour)
	endTime := now
	var err error
	if raw := r.URL.Query().Get("start_time"); raw != "" {
		startTime, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			http.Error(w, "invalid start_time format (use RFC3339)", http.StatusBadRequest)
			return
		}
	}
	if raw := r.URL.Query().Get("end_time"); raw != "" {
		endTime, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			http.Error(w, "invalid end_time format (use RFC3339)", http.StatusBadRequest)
			return
		}
	}
	if !startTime.Before(endTime) {
		http.Error(w, "start_time must be before end_time", http.StatusBadRequest)
		return
	}

	type point struct {
		Timestamp      string  `json:"timestamp"`
		Temperature    float64 `json:"temperature_c,omitempty"`
		Humidity       float64 `json:"humidity_pct,omitempty"`
		AvgTemperature float64 `json:"avg_temperature_c,omitempty"`
		MinTemperature float64 `json:"min_temperature_c,omitempty"`
		MaxTemperature float64 `json:"max_temperature_c,omitempty"`
		AvgHumidity    float64 `json:"avg_humidity_pct,omitempty"`
		MinHumidity    float64 `json:"min_humidity_pct,omitempty"`
		MaxHumidity    float64 `json:"max_humidity_pct,omitempty"`
		Count          int     `json:"count,omitempty"`
	}
	points := make([]point, 0)

	if interval == "raw" {
		rows, queryErr := db.Query(`
			SELECT created_at, temperature_c, humidity_pct
			FROM environment_readings
			WHERE sensor_id = ? AND created_at BETWEEN ? AND ?
			ORDER BY created_at
		`, sensorID, startTime.In(tzLocation), endTime.In(tzLocation))
		if queryErr != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var timestamp time.Time
			var temperature, humidity float64
			if err := rows.Scan(&timestamp, &temperature, &humidity); err != nil {
				log.Printf("[environment] analytics scan error: %v", err)
				continue
			}
			points = append(points, point{
				Timestamp:   timestamp.In(tzLocation).Format(time.RFC3339),
				Temperature: temperature,
				Humidity:    humidity,
			})
		}
	} else {
		rows, queryErr := db.Query(`
			SELECT hour_timestamp,
			       avg_temperature_c, min_temperature_c, max_temperature_c,
			       avg_humidity_pct, min_humidity_pct, max_humidity_pct, count
			FROM environment_readings_hourly
			WHERE sensor_id = ? AND hour_timestamp BETWEEN ? AND ?
			ORDER BY hour_timestamp
		`, sensorID, startTime.In(tzLocation), endTime.In(tzLocation))
		if queryErr != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var timestamp time.Time
			var p point
			if err := rows.Scan(
				&timestamp,
				&p.AvgTemperature, &p.MinTemperature, &p.MaxTemperature,
				&p.AvgHumidity, &p.MinHumidity, &p.MaxHumidity, &p.Count,
			); err != nil {
				log.Printf("[environment] hourly analytics scan error: %v", err)
				continue
			}
			p.Timestamp = timestamp.In(tzLocation).Format(time.RFC3339)
			points = append(points, p)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sensor_id": sensorID, "interval": interval, "count": len(points),
		"start_time": startTime.Format(time.RFC3339), "end_time": endTime.Format(time.RFC3339),
		"data": points,
	})
}

func checkEnvironmentAlerts(sensor environmentSensor, reading environmentReading) {
	check := func(metric string, value float64, low, high *float64, unit string) {
		state := "normal"
		if low != nil && value <= *low {
			state = "low"
		} else if high != nil && value >= *high {
			state = "high"
		}
		key := sensor.ID + ":" + metric
		environmentNotifyStateMu.Lock()
		previous := environmentNotifyState[key]
		environmentNotifyState[key] = state
		environmentNotifyStateMu.Unlock()
		if state == "normal" || state == previous {
			return
		}
		message := fmt.Sprintf("%s alert: %s %s is %.1f%s",
			strings.ToUpper(state), sensor.Name, metric, value, unit)
		log.Printf("[environment] %s", message)
		sendTelegramNotification(message)
	}
	check("temperature", reading.Temperature, sensor.TemperatureLow, sensor.TemperatureHigh, " C")
	check("humidity", reading.Humidity, sensor.HumidityLow, sensor.HumidityHigh, "%")
}

func aggregateEnvironmentHourlyReadings() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		environmentSensorsMu.RLock()
		sensorIDs := append([]string(nil), environmentSensorOrder...)
		environmentSensorsMu.RUnlock()
		windowEnd := time.Now().In(tzLocation).Truncate(time.Hour)
		for _, sensorID := range sensorIDs {
			if err := aggregateEnvironmentSensorHours(sensorID, windowEnd); err != nil {
				log.Printf("[environment] aggregate sensor %s: %v", sensorID, err)
			}
		}
	}
}

func aggregateEnvironmentSensorHours(sensorID string, windowEnd time.Time) error {
	var lastAggregated, firstReading sql.NullTime
	if err := db.QueryRow(`
		SELECT MAX(hour_timestamp) FROM environment_readings_hourly WHERE sensor_id = ?
	`, sensorID).Scan(&lastAggregated); err != nil {
		return err
	}
	startHour := time.Time{}
	if lastAggregated.Valid {
		startHour = lastAggregated.Time.In(tzLocation).Add(time.Hour).Truncate(time.Hour)
	} else {
		if err := db.QueryRow(`
			SELECT MIN(created_at) FROM environment_readings WHERE sensor_id = ?
		`, sensorID).Scan(&firstReading); err != nil {
			return err
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
		SELECT DATE_FORMAT(created_at, '%Y-%m-%d %H:00:00'),
		       AVG(temperature_c), MIN(temperature_c), MAX(temperature_c),
		       AVG(humidity_pct), MIN(humidity_pct), MAX(humidity_pct), COUNT(*)
		FROM environment_readings
		WHERE sensor_id = ? AND created_at >= ? AND created_at < ?
		GROUP BY DATE_FORMAT(created_at, '%Y-%m-%d %H:00:00')
		ORDER BY 1
	`, sensorID, startHour, windowEnd)
	if err != nil {
		return err
	}
	defer rows.Close()

	stmt, err := db.Prepare(`
		INSERT INTO environment_readings_hourly (
			sensor_id, hour_timestamp,
			avg_temperature_c, min_temperature_c, max_temperature_c,
			avg_humidity_pct, min_humidity_pct, max_humidity_pct, count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			avg_temperature_c = VALUES(avg_temperature_c),
			min_temperature_c = VALUES(min_temperature_c),
			max_temperature_c = VALUES(max_temperature_c),
			avg_humidity_pct = VALUES(avg_humidity_pct),
			min_humidity_pct = VALUES(min_humidity_pct),
			max_humidity_pct = VALUES(max_humidity_pct),
			count = VALUES(count)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for rows.Next() {
		var hour string
		var avgT, minT, maxT, avgH, minH, maxH float64
		var count int
		if err := rows.Scan(&hour, &avgT, &minT, &maxT, &avgH, &minH, &maxH, &count); err != nil {
			return err
		}
		timestamp, err := time.ParseInLocation("2006-01-02 15:04:05", hour, tzLocation)
		if err != nil {
			return err
		}
		if _, err := stmt.Exec(sensorID, timestamp, avgT, minT, maxT, avgH, minH, maxH, count); err != nil {
			return err
		}
	}
	return rows.Err()
}
