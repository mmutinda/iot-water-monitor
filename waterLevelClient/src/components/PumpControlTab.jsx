import { useCallback, useEffect, useState } from 'react';
import { authFetch } from '../utils/auth';
import { distanceToLevel } from '../utils/calculations';
import { subscribeToPump } from '../utils/sseClient';
import PumpSettings from './PumpSettings';
import './PumpControlTab.css';

// While the pump is ON we re-send "on" as a keep-alive. The relay auto-stops
// ~1 min after the last command, so this must be comfortably under that.
const KEEPALIVE_MS = 30000;
// Source tank the pump draws from, and the level below which we warn about a
// dry run (matches the server's low-water alert threshold).
const SOURCE_TANK = 'B';
const DRY_RUN_PCT = 15;

export default function PumpControlTab({ tankMap = {}, canControl = true }) {
  const [pumpPendingAction, setPumpPendingAction] = useState(null);
  const [pumpStatus, setPumpStatus] = useState(null);
  // Relay switch state as reported by the controller (not measured water flow).
  const [pumpState, setPumpState] = useState(null); // "on" | "off" | "offline" | "unknown" | null (loading)
  const [intendedOn, setIntendedOn] = useState(false); // user wants it running → drives keep-alive
  const [dryRunPct, setDryRunPct] = useState(null); // non-null → confirmation pending

  const refreshState = useCallback(async () => {
    try {
      const response = await authFetch('/pump');
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const data = await response.json();
      setPumpState(data.state || 'unknown');
    } catch {
      setPumpState('unknown');
    }
  }, []);

  // Live pump state over SSE — no polling. The server pushes every change
  // (commands, the firmware watchdog auto-off, the offline Last Will, or another
  // client's action), so the indicator reflects the relay in real time and the
  // post-command race is gone. One fetch on mount seeds the initial value in
  // case the SSE snapshot is briefly delayed.
  //
  // If the relay is reported off (or offline) — e.g. another device stopped the
  // pump — drop our keep-alive intent so this client never resurrects a pump
  // someone else deliberately stopped.
  useEffect(() => {
    refreshState();
    return subscribeToPump((data) => {
      const next = data.state || 'unknown';
      setPumpState(next);
      if (next === 'off' || next === 'offline') setIntendedOn(false);
    });
  }, [refreshState]);

  // If the pump is already running when this view loads (e.g. a page reload
  // mid-fill), adopt that intent so the keep-alive keeps it on instead of
  // letting the relay's watchdog stop it a minute later.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await authFetch('/pump');
        if (!res.ok) return;
        const data = await res.json();
        if (!cancelled && data.state === 'on') setIntendedOn(true);
      } catch {
        // ignore — best effort
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // Low-level command. Returns { ok, tankFull, message }.
  const publishCommand = useCallback(async (action) => {
    try {
      const response = await authFetch('/pump', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action }),
      });
      if (response.ok) return { ok: true };

      let body = {};
      try {
        body = await response.json();
      } catch {
        // non-JSON error body
      }
      return {
        ok: false,
        code: body.code,
        tankFull: body.code === 'tank_full',
        offline: body.code === 'controller_offline',
        message: body.error || `HTTP ${response.status}`,
      };
    } catch (err) {
      return { ok: false, message: err.message };
    }
  }, []);

  const startPump = useCallback(async () => {
    setPumpPendingAction('on');
    setPumpStatus(null);

    const result = await publishCommand('on');
    if (result.ok) {
      setIntendedOn(true); // begin keep-alive renewals
      setPumpStatus({ type: 'success', message: 'Pump start command sent.' });
      // No manual refresh — the relay's confirmed state arrives over SSE.
    } else {
      setIntendedOn(false);
      setPumpStatus({
        type: 'error',
        message: result.tankFull || result.offline ? result.message : `Pump command failed: ${result.message}`,
      });
    }
    setPumpPendingAction(null);
  }, [publishCommand]);

  // Start button: warn first if the source tank is near empty (dry-run risk).
  const handleStart = useCallback(async () => {
    try {
      const res = await authFetch(`/latest?tank=${SOURCE_TANK}`);
      if (res.ok) {
        const reading = await res.json();
        const pct = distanceToLevel(reading.distance, tankMap[SOURCE_TANK]);
        if (Number.isFinite(pct) && pct <= DRY_RUN_PCT) {
          setDryRunPct(pct); // open confirmation panel instead of starting
          return;
        }
      }
      // If we can't read the source level, fall through and let the user start;
      // the firmware watchdog still bounds how long it can run.
    } catch {
      // ignore — best-effort guard
    }
    startPump();
  }, [startPump, tankMap]);

  const confirmDryRun = useCallback(() => {
    setDryRunPct(null);
    startPump();
  }, [startPump]);

  const cancelDryRun = useCallback(() => setDryRunPct(null), []);

  const stopPump = useCallback(async () => {
    setIntendedOn(false); // stop keep-alive
    setPumpPendingAction('off');
    setPumpStatus(null);

    const result = await publishCommand('off');
    setPumpStatus(
      result.ok
        ? { type: 'success', message: 'Pump stop command sent.' }
        : { type: 'error', message: result.offline ? result.message : `Pump command failed: ${result.message}` },
    );
    // No manual refresh — the relay's confirmed state arrives over SSE.
    setPumpPendingAction(null);
  }, [publishCommand]);

  // Keep-alive: while the user wants the pump on, re-send "on" so the relay's
  // safety timer never elapses. Before each renewal we re-check the relay's
  // actual state: if another device (or the interlock) already turned the pump
  // off — including while this tab was backgrounded and the timer was suspended
  // — we drop our intent instead of restarting it. This prevents a woken-up tab
  // from resurrecting a pump that was deliberately stopped on another device.
  useEffect(() => {
    if (!intendedOn) return undefined;
    const id = setInterval(async () => {
      // Authoritative check first. On a network hiccup, skip this renewal rather
      // than risk a blind restart.
      let state;
      try {
        const res = await authFetch('/pump');
        if (!res.ok) return;
        state = (await res.json()).state || 'unknown';
      } catch {
        return;
      }
      if (state !== 'on') {
        setPumpState(state);
        setIntendedOn(false);
        return;
      }
      const result = await publishCommand('on');
      // Stop renewing if the tank filled or the controller dropped offline —
      // in both cases the relay is no longer (safely) running the pump.
      if (!result.ok && (result.tankFull || result.offline)) {
        setIntendedOn(false);
        setPumpStatus({ type: 'error', message: result.message });
        refreshState();
      }
    }, KEEPALIVE_MS);
    return () => clearInterval(id);
  }, [intendedOn, publishCommand, refreshState]);

  const isOffline = pumpState === 'offline';
  const stateLabel =
    pumpState === 'on' ? 'ON' : pumpState === 'off' ? 'OFF' : isOffline ? 'Offline' : 'Unknown';
  const stateClass =
    pumpState === 'on' ? 'is-on' : pumpState === 'off' ? 'is-off' : isOffline ? 'is-offline' : 'is-unknown';

  return (
    <div className="pump-control-tab">
      <div className="pump-control-card">
        <header className="pump-control-header">
          <h2>Water Pump Control</h2>
          <p className="pump-control-description">Start or stop the water pump</p>
        </header>

        <div className="pump-control-body">
          <div className={`pump-state-indicator ${stateClass}`}>
            <span className="pump-state-dot" />
            <span className="pump-state-text">
              {pumpState === null
                ? 'Relay is checking…'
                : isOffline
                  ? 'Controller offline'
                  : `Relay is ${stateLabel}`}
            </span>
          </div>

          {pumpState !== null && !isOffline ? (
            <p className="pump-state-caption">
              Shows the relay switch the controller reports — not measured water
              flow. A welded contact or a jammed pump can’t be detected here.
            </p>
          ) : null}

          {isOffline ? (
            <p className="pump-offline-note">
              The pump controller isn’t connected (power or Wi-Fi loss). Commands
              can’t reach it right now — its last known state is shown above.
            </p>
          ) : null}

          {intendedOn ? (
            <p className="pump-keepalive-note">
              Keeping the pump running. The server holds it on until you press
              Stop or the destination tank is full — it keeps running even if you
              lock your phone or close this page.
            </p>
          ) : null}

          {!canControl ? (
            <p className="pump-readonly-note">
              You’re signed in as a read-only viewer. Starting or stopping the
              pump and changing settings require a superadmin account.
            </p>
          ) : dryRunPct !== null ? (
            <div className="pump-confirm">
              <p className="pump-confirm-text">
                ⚠️ Source {tankMap[SOURCE_TANK]?.name || `tank ${SOURCE_TANK}`} is
                low ({dryRunPct.toFixed(0)}%). Running the pump may let it run dry
                and damage the motor. Start anyway?
              </p>
              <div className="pump-confirm-actions">
                <button type="button" className="pump-button pump-button-start" onClick={confirmDryRun}>
                  Start anyway
                </button>
                <button type="button" className="pump-button pump-button-cancel" onClick={cancelDryRun}>
                  Cancel
                </button>
              </div>
            </div>
          ) : (
            <div className="pump-controls">
              <button
                type="button"
                className="pump-button pump-button-start"
                onClick={handleStart}
                disabled={pumpPendingAction !== null}
              >
                {pumpPendingAction === 'on' ? 'Starting…' : 'Start Pump'}
              </button>
              <button
                type="button"
                className="pump-button pump-button-stop"
                onClick={stopPump}
                disabled={pumpPendingAction !== null}
              >
                {pumpPendingAction === 'off' ? 'Stopping…' : 'Stop Pump'}
              </button>
            </div>
          )}

          {pumpStatus ? (
            <div className={`pump-status ${pumpStatus.type === 'error' ? 'is-error' : 'is-success'}`}>
              {pumpStatus.message}
            </div>
          ) : null}

          {canControl ? <PumpSettings tanks={Object.values(tankMap)} /> : null}
        </div>
      </div>
    </div>
  );
}
