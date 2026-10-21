import { useCallback, useEffect, useState } from 'react';
import { authFetch } from '../utils/auth';
import { subscribeToEnvironment } from '../utils/sseClient';

export function useEnvironmentSensors() {
  const [sensors, setSensors] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    authFetch('/environment/sensors')
      .then((response) => {
        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        return response.json();
      })
      .then((json) => {
        setSensors(Array.isArray(json.data) ? json.data : []);
        setError(null);
      })
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false));
  }, []);

  return { sensors, loading, error };
}

export function useEnvironmentReading(sensorId) {
  const [reading, setReading] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    if (!sensorId) {
      setReading(null);
      setLoading(false);
      return;
    }
    setLoading(true);
    authFetch(`/environment/latest?sensor=${encodeURIComponent(sensorId)}`)
      .then((response) => {
        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        return response.json();
      })
      .then((json) => {
        setReading(json);
        setError(null);
      })
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false));
  }, [sensorId]);

  useEffect(() => {
    if (!sensorId) return undefined;
    return subscribeToEnvironment((nextReading) => {
      if (nextReading.sensor_id !== sensorId) return;
      setReading(nextReading);
      setError(null);
      setLoading(false);
    });
  }, [sensorId]);

  return { reading, loading, error };
}

export function useEnvironmentAnalytics(sensorId, range, interval) {
  const [data, setData] = useState([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);

  const refresh = useCallback(async () => {
    if (!sensorId) {
      setData([]);
      return;
    }
    const hours = range === '30d' ? 24 * 30 : range === '7d' ? 24 * 7 : 24;
    const end = new Date();
    const start = new Date(end.getTime() - hours * 60 * 60 * 1000);
    const params = new URLSearchParams({
      sensor: sensorId,
      start_time: start.toISOString(),
      end_time: end.toISOString(),
      interval,
    });

    setLoading(true);
    setError(null);
    try {
      const response = await authFetch(`/environment/analytics?${params.toString()}`);
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const json = await response.json();
      setData(Array.isArray(json.data) ? json.data : []);
    } catch (err) {
      setData([]);
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }, [interval, range, sensorId]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  return { data, loading, error, refresh };
}
