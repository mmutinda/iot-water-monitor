import { useState, useEffect } from 'react';
import { authFetch } from '../utils/auth';
import { subscribeToTank } from '../utils/sseClient';

const API_URL = '/latest';

/**
 * Returns the latest water level reading for the given tank.
 * - Makes one HTTP request on mount to get the current value immediately.
 * - Then receives live push updates via the shared SSE connection; no polling.
 */
export function useWaterLevel(tank) {
  const [data, setData] = useState(null);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);

  // One-time fetch to populate immediately before the first SSE event arrives
  useEffect(() => {
    if (!tank) {
      setLoading(false);
      return;
    }
    setLoading(true);
    authFetch(`${API_URL}?tank=${encodeURIComponent(tank)}`)
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json();
      })
      .then((json) => {
        setData(json);
        setError(null);
      })
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false));
  }, [tank]);

  // Subscribe to live SSE updates for this tank
  useEffect(() => {
    if (!tank) return;
    return subscribeToTank(tank, (reading) => {
      setData(reading);
      setError(null);
      setLoading(false);
    });
  }, [tank]);

  return { data, error, loading };
}
