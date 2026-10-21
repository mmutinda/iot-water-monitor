import { useCallback, useEffect, useState } from "react";
import { authFetch } from "../utils/auth";

const API_URL = "/analytics";

const RANGE_PRESETS = {
  "24h": { label: "Last 24 hours", hours: 24 },
  "7d": { label: "Last 7 days", hours: 24 * 7 },
  "30d": { label: "Last 30 days", hours: 24 * 30 },
};

function computeWindow(rangeKey) {
  const preset = RANGE_PRESETS[rangeKey] ?? RANGE_PRESETS["24h"];
  const end = new Date();
  const start = new Date(end.getTime() - preset.hours * 60 * 60 * 1000);
  return { start, end, label: preset.label };
}

export function useAnalytics(rangeKey, interval, tank) {
  const [data, setData] = useState([]);
  const [meta, setMeta] = useState(null);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(false);
  const [windowInfo, setWindowInfo] = useState(() => computeWindow(rangeKey));

  useEffect(() => {
    setWindowInfo(computeWindow(rangeKey));
  }, [rangeKey]);

  const fetchAnalytics = useCallback(async () => {
    setLoading(true);
    setError(null);

    const currentWindow = computeWindow(rangeKey);
    setWindowInfo(currentWindow);

    const params = new URLSearchParams({
      start_time: currentWindow.start.toISOString(),
      end_time: currentWindow.end.toISOString(),
      interval,
    });
    if (tank) {
      params.set("tank", tank);
    }

    try {
      const res = await authFetch(`${API_URL}?${params.toString()}`);
      if (!res.ok) {
        throw new Error(`HTTP ${res.status}`);
      }
      const json = await res.json();
      setData(Array.isArray(json.data) ? json.data : []);
      setMeta({
        count: json.count ?? 0,
        interval: json.interval ?? interval,
        startTime: json.start_time ?? currentWindow.start.toISOString(),
        endTime: json.end_time ?? currentWindow.end.toISOString(),
      });
    } catch (err) {
      setError(err.message);
      setData([]);
      setMeta(null);
    } finally {
      setLoading(false);
    }
  }, [interval, rangeKey, tank]);

  useEffect(() => {
    fetchAnalytics();
  }, [fetchAnalytics]);

  return {
    data,
    meta,
    error,
    loading,
    refresh: fetchAnalytics,
    windowInfo,
  };
}
