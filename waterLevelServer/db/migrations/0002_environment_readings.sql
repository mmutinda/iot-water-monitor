-- Migration 0002: add temperature/humidity sensors, history, and hourly aggregates.
--
-- Run once:
--   mysql -u root -p water_sensor < db/migrations/0002_environment_readings.sql

CREATE TABLE environment_sensors (
  id VARCHAR(64) PRIMARY KEY,
  name VARCHAR(64) NOT NULL,
  mqtt_topic VARCHAR(128) NOT NULL UNIQUE,
  temperature_low DOUBLE NULL,
  temperature_high DOUBLE NULL,
  humidity_low DOUBLE NULL,
  humidity_high DOUBLE NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE environment_readings (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  sensor_id VARCHAR(64) NOT NULL,
  temperature_c DOUBLE NOT NULL,
  humidity_pct DOUBLE NOT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_environment_sensor_time (sensor_id, created_at DESC),
  INDEX idx_environment_created_at (created_at DESC),
  CONSTRAINT fk_environment_sensor
    FOREIGN KEY (sensor_id) REFERENCES environment_sensors(id)
    ON UPDATE CASCADE ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE environment_readings_hourly (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  sensor_id VARCHAR(64) NOT NULL,
  hour_timestamp TIMESTAMP NOT NULL,
  avg_temperature_c DOUBLE NOT NULL,
  min_temperature_c DOUBLE NOT NULL,
  max_temperature_c DOUBLE NOT NULL,
  avg_humidity_pct DOUBLE NOT NULL,
  min_humidity_pct DOUBLE NOT NULL,
  max_humidity_pct DOUBLE NOT NULL,
  count INT NOT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_environment_sensor_hour (sensor_id, hour_timestamp),
  INDEX idx_environment_hour (hour_timestamp DESC),
  CONSTRAINT fk_environment_hourly_sensor
    FOREIGN KEY (sensor_id) REFERENCES environment_sensors(id)
    ON UPDATE CASCADE ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT INTO environment_sensors (
  id, name, mqtt_topic,
  temperature_low, temperature_high, humidity_low, humidity_high
) VALUES (
  'room-1', 'Room Sensor', 'environment/room-1/readings',
  15, 32, 30, 70
);
