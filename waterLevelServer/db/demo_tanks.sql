-- Synthetic tank configuration for a local demonstration.
INSERT INTO tank_config (
  tank, name, mqtt_topic, total_height, sensor_offset,
  min_distance, max_distance, capacity_liters
) VALUES
  ('A', 'Demo Tank A', 'tank_a/level', 200, 5, 5, 195, 1000),
  ('B', 'Demo Tank B', 'tank_b/level', 200, 5, 5, 195, 1000)
ON DUPLICATE KEY UPDATE tank = VALUES(tank);
