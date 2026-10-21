-- Tank configuration (all values in centimeters)
CREATE TABLE IF NOT EXISTS tank_config (
  id INT AUTO_INCREMENT PRIMARY KEY,
  tank ENUM('A','B') NOT NULL UNIQUE,   -- stable identifier (used in API, readings, pump config)
  name VARCHAR(64) NOT NULL DEFAULT '',   -- human-readable label shown in the UI / notifications
  mqtt_topic VARCHAR(128) NOT NULL DEFAULT '', -- topic the server subscribes to for this tank
  total_height DOUBLE NOT NULL,
  sensor_offset DOUBLE NOT NULL,
  min_distance DOUBLE NOT NULL,
  max_distance DOUBLE NOT NULL,
  capacity_liters DOUBLE NOT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Generic key/value application settings (e.g. pump interlock config)
CREATE TABLE IF NOT EXISTS app_settings (
  setting_key   VARCHAR(64) PRIMARY KEY,
  setting_value VARCHAR(255) NOT NULL,
  updated_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Raw readings from sensors
CREATE TABLE IF NOT EXISTS readings (
  id INT AUTO_INCREMENT PRIMARY KEY,
  tank ENUM('A','B') NOT NULL,
  distance DOUBLE NOT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_readings_tank_time (tank, created_at DESC),
  INDEX idx_readings_created_at (created_at DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Hourly aggregates for faster analytics
CREATE TABLE IF NOT EXISTS readings_hourly (
  id INT AUTO_INCREMENT PRIMARY KEY,
  tank ENUM('A','B') NOT NULL,
  hour_timestamp TIMESTAMP NOT NULL,
  avg_distance DOUBLE NOT NULL,
  min_distance DOUBLE NOT NULL,
  max_distance DOUBLE NOT NULL,
  count INT NOT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_tank_hour (tank, hour_timestamp),
  INDEX idx_hourly_hour (hour_timestamp DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Temperature/humidity sensor configuration and alert thresholds
CREATE TABLE IF NOT EXISTS environment_sensors (
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

-- Raw temperature/humidity readings
CREATE TABLE IF NOT EXISTS environment_readings (
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

-- Hourly environmental aggregates for longer chart ranges
CREATE TABLE IF NOT EXISTS environment_readings_hourly (
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
)
ON DUPLICATE KEY UPDATE id = VALUES(id);
