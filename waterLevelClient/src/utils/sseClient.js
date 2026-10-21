import { getToken, clearAuth } from './auth';

// Map of tankId → Set of listener callbacks
const listeners = new Map();
// Set of callbacks for pump-state events (the named "pump" SSE event).
const pumpListeners = new Set();
const environmentListeners = new Set();

let eventSource = null;

function connect() {
  const token = getToken();
  if (!token) return;

  eventSource = new EventSource(`/events?token=${encodeURIComponent(token)}`);

  eventSource.onmessage = (e) => {
    try {
      const reading = JSON.parse(e.data);
      const cbs = listeners.get(reading.tank);
      if (cbs) cbs.forEach((cb) => cb(reading));
    } catch {
      // ignore malformed frames
    }
  };

  // Pump state arrives on its own named event ({ state, timestamp }).
  eventSource.addEventListener('pump', (e) => {
    try {
      const pump = JSON.parse(e.data);
      pumpListeners.forEach((cb) => cb(pump));
    } catch {
      // ignore malformed frames
    }
  });

  eventSource.addEventListener('environment', (e) => {
    try {
      const reading = JSON.parse(e.data);
      environmentListeners.forEach((cb) => cb(reading));
    } catch {
      // ignore malformed frames
    }
  });

  eventSource.onerror = (e) => {
    // 401 — token invalid or expired
    if (e.target.readyState === EventSource.CLOSED) {
      eventSource = null;
      const token = getToken();
      if (!token) return;
      // Back-off reconnect (3 s)
      setTimeout(connect, 3000);
    }
  };
}

export function subscribeToTank(tankId, callback) {
  if (!listeners.has(tankId)) listeners.set(tankId, new Set());
  listeners.get(tankId).add(callback);

  if (!eventSource) connect();

  return () => {
    listeners.get(tankId)?.delete(callback);
  };
}

// Subscribe to live pump-state pushes. callback receives { state, timestamp }.
export function subscribeToPump(callback) {
  pumpListeners.add(callback);

  if (!eventSource) connect();

  return () => {
    pumpListeners.delete(callback);
  };
}

export function subscribeToEnvironment(callback) {
  environmentListeners.add(callback);

  if (!eventSource) connect();

  return () => {
    environmentListeners.delete(callback);
  };
}

export function disconnectSSE() {
  eventSource?.close();
  eventSource = null;
}
