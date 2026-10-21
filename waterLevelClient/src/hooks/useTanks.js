import { useCallback, useEffect, useMemo, useState } from "react";
import { DEFAULT_TANK_CONFIG } from "../utils/calculations";
import { authFetch } from "../utils/auth";

const API_URL = "/tanks";

// Live Monitor / selector display order: Rooftop tank (B) first, then Ground
// (A); any other tank follows, ordered by id. Lower rank sorts earlier.
const TANK_DISPLAY_ORDER = ["B", "A"];

function displayRank(id) {
  const i = TANK_DISPLAY_ORDER.indexOf(id);
  return i >= 0 ? i : TANK_DISPLAY_ORDER.length;
}

function normalizeTankConfig(raw) {
  if (!raw || typeof raw !== "object") {
    return null;
  }

  const capacityCandidate =
    raw.capacity_liters ?? raw.capacityLiters ?? raw.capacity ?? null;

  const capacityLiters = (() => {
    if (capacityCandidate === null || capacityCandidate === undefined) {
      return DEFAULT_TANK_CONFIG.capacityLiters;
    }
    const numeric = Number(capacityCandidate);
    if (!Number.isFinite(numeric)) {
      return DEFAULT_TANK_CONFIG.capacityLiters;
    }
    return numeric;
  })();

  return {
    id: raw.tank,
    tank: raw.tank,
    name: raw.name && String(raw.name).trim() ? String(raw.name).trim() : raw.tank,
    totalHeight: Number(raw.total_height ?? DEFAULT_TANK_CONFIG.totalHeight),
    sensorOffset: Number(raw.sensor_offset ?? DEFAULT_TANK_CONFIG.sensorOffset),
    minDistance: Number(raw.min_distance ?? DEFAULT_TANK_CONFIG.minDistance),
    maxDistance: Number(raw.max_distance ?? DEFAULT_TANK_CONFIG.maxDistance),
    capacityLiters,
    raw,
  };
}

export function useTanks() {
  const [tanks, setTanks] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const fetchTanks = useCallback(async () => {
    setLoading(true);
    setError(null);

    try {
      const res = await authFetch(API_URL);
      if (!res.ok) {
        throw new Error(`HTTP ${res.status}`);
      }
      const json = await res.json();
      const entries = Array.isArray(json.data) ? json.data : [];
      const normalized = entries
        .map(normalizeTankConfig)
        .filter((cfg) => cfg && cfg.id)
        .sort((a, b) => displayRank(a.id) - displayRank(b.id) || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
      setTanks(normalized);
    } catch (err) {
      setError(err.message);
      setTanks([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchTanks();
  }, [fetchTanks]);

  const tankMap = useMemo(() => {
    return tanks.reduce((acc, cfg) => {
      acc[cfg.id] = cfg;
      return acc;
    }, {});
  }, [tanks]);

  return {
    tanks,
    tankMap,
    loading,
    error,
    refresh: fetchTanks,
  };
}
