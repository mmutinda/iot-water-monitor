// Default tank configuration (all distances in cm)
export const DEFAULT_TANK_CONFIG = {
  totalHeight: 200,
  sensorOffset: 5,
  minDistance: 5,
  maxDistance: 195,
  capacityLiters: 1000,
};

function resolveTankConfig(config) {
  if (!config) {
    return DEFAULT_TANK_CONFIG;
  }

  return {
    totalHeight: isFinite(config.totalHeight)
      ? Number(config.totalHeight)
      : DEFAULT_TANK_CONFIG.totalHeight,
    sensorOffset: isFinite(config.sensorOffset)
      ? Number(config.sensorOffset)
      : DEFAULT_TANK_CONFIG.sensorOffset,
    minDistance: isFinite(config.minDistance)
      ? Number(config.minDistance)
      : DEFAULT_TANK_CONFIG.minDistance,
    maxDistance: isFinite(config.maxDistance)
      ? Number(config.maxDistance)
      : DEFAULT_TANK_CONFIG.maxDistance,
    capacityLiters:
      config.capacityLiters !== null &&
      config.capacityLiters !== undefined &&
      isFinite(config.capacityLiters)
        ? Number(config.capacityLiters)
        : DEFAULT_TANK_CONFIG.capacityLiters,
  };
}

/**
 * Convert ultrasonic sensor distance (cm) to water level percentage.
 * The sensor sits at the top of the tank and measures distance to the water surface.
 * Higher distance = lower water level.
 *
 * @param {number} distance - Sensor distance reading in cm
 * @returns {number} Water level percentage (0–100), clamped
 */
export function distanceToLevel(distance, config) {
  const cfg = resolveTankConfig(config);
  const distanceFromMax = distance - cfg.sensorOffset;
  const waterHeight = cfg.maxDistance - distanceFromMax;
  const percentage = (waterHeight / cfg.maxDistance) * 100;
  return Math.max(0, Math.min(100, Math.round(percentage * 10) / 10));
}

/*
export function distanceToLevel(distance, config) {
  const cfg = resolveTankConfig(config);
  const range = cfg.maxDistance - cfg.minDistance;
  if (range <= 0) {
    return 0;
  }
  const level = ((cfg.maxDistance - distance) / range) * 100;
  return Math.max(0, Math.min(100, Math.round(level * 10) / 10));
}
*/

/**
 * Convert water level percentage to an estimated volume in liters.
 * Assumes a cylindrical tank with ~1000L capacity at 100%.
 * Adjust TANK_CAPACITY_LITERS to match your actual tank.
 *
 * @param {number} levelPercent - Water level as a percentage
 * @returns {number} Estimated volume in liters
 */
export function levelToVolume(levelPercent, config) {
  const cfg = resolveTankConfig(config);
  return Math.round((levelPercent / 100) * cfg.capacityLiters);
}

/**
 * Determine the status category based on water level.
 *
 * @param {number} levelPercent
 * @returns {{ label: string, color: string, bgColor: string }}
 */
export function getStatus(levelPercent) {
  if (levelPercent <= 10)
    return {
      label: "CRITICAL",
      color: "#ff4444",
      bgColor: "rgba(255,68,68,0.15)",
    };
  if (levelPercent <= 25)
    return { label: "LOW", color: "#ff8c00", bgColor: "rgba(255,140,0,0.15)" };
  if (levelPercent <= 75)
    return {
      label: "NORMAL",
      color: "#44cc44",
      bgColor: "rgba(68,204,68,0.15)",
    };
  if (levelPercent <= 90)
    return {
      label: "HIGH",
      color: "#4488ff",
      bgColor: "rgba(68,136,255,0.15)",
    };
  return { label: "FULL", color: "#aa44ff", bgColor: "rgba(170,68,255,0.15)" };
}

/**
 * Get water color based on level percentage.
 */
export function getWaterColor(levelPercent) {
  if (levelPercent <= 10) return { fill: "#ff4444", wave: "#ff6666" };
  if (levelPercent <= 25) return { fill: "#ff8c00", wave: "#ffa633" };
  if (levelPercent <= 50) return { fill: "#3399ff", wave: "#55aaff" };
  return { fill: "#2277dd", wave: "#3399ff" };
}

/**
 * Format a timestamp string for display.
 */
export function formatTimestamp(ts) {
  if (!ts) return "—";
  const d = new Date(ts);
  return d.toLocaleTimeString("en-GB", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

export function formatDistance(distance) {
  if (distance === null || distance === undefined) {
    return "—";
  }
  const value = Number(distance);
  if (!Number.isFinite(value)) {
    return "—";
  }
  return value.toFixed(2);
}
