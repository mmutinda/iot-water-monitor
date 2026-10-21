-- Migration 0001: give each tank a human-readable name and an explicit MQTT topic.
--
-- The `tank` letter stays the stable identifier; `name` is what users see in the
-- dashboard and notifications, and `mqtt_topic` is the topic the server subscribes
-- to for that tank's readings.
--
-- Safe on existing data (additive only). Run ONCE — MySQL's ADD COLUMN is not
-- idempotent. Fresh installs already get these columns from schema.sql.
--
--   mysql -u root -p water_sensor < db/migrations/0001_tank_name_topic.sql

ALTER TABLE tank_config
  ADD COLUMN name       VARCHAR(64)  NOT NULL DEFAULT '' AFTER tank,
  ADD COLUMN mqtt_topic VARCHAR(128) NOT NULL DEFAULT '' AFTER name;

UPDATE tank_config SET name = 'Rooftop Tank', mqtt_topic = 'tank_a/level' WHERE tank = 'A';
UPDATE tank_config SET name = 'Ground Tank',  mqtt_topic = 'tank_b/level' WHERE tank = 'B';
