import { useEffect, useState } from 'react';
import { authFetch } from '../utils/auth';

export default function PumpSettings({ tanks = [] }) {
  const [open, setOpen] = useState(false);
  const [destTank, setDestTank] = useState('');
  const [fullPct, setFullPct] = useState(90);
  // Auto-fill
  const [autoEnabled, setAutoEnabled] = useState(false);
  const [autoStartPct, setAutoStartPct] = useState(15);
  const [autoStopPct, setAutoStopPct] = useState(80);
  const [autoSourceTank, setAutoSourceTank] = useState('');
  const [autoSourceMin, setAutoSourceMin] = useState(15);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await authFetch('/pump/config');
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        const data = await res.json();
        if (cancelled) return;
        setDestTank(data.dest_tank || '');
        setFullPct(typeof data.full_pct === 'number' ? data.full_pct : 90);
        setAutoEnabled(!!data.auto_enabled);
        if (typeof data.auto_start_pct === 'number') setAutoStartPct(data.auto_start_pct);
        if (typeof data.auto_stop_pct === 'number') setAutoStopPct(data.auto_stop_pct);
        setAutoSourceTank(data.auto_source_tank || '');
        if (typeof data.auto_source_min_pct === 'number') setAutoSourceMin(data.auto_source_min_pct);
      } catch (err) {
        if (!cancelled) setStatus({ type: 'error', message: `Could not load settings: ${err.message}` });
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const save = async () => {
    const pct = Number(fullPct);
    if (destTank !== '' && (!Number.isFinite(pct) || pct < 0 || pct > 100)) {
      setStatus({ type: 'error', message: 'Full % must be between 0 and 100.' });
      return;
    }
    const startPct = Number(autoStartPct);
    const stopPct = Number(autoStopPct);
    const sourceMin = Number(autoSourceMin);
    if (autoEnabled) {
      if (!destTank) {
        setStatus({ type: 'error', message: 'Set a destination tank before enabling auto-fill.' });
        return;
      }
      if (!Number.isFinite(startPct) || !Number.isFinite(stopPct) || startPct >= stopPct) {
        setStatus({ type: 'error', message: 'Auto start % must be below stop %.' });
        return;
      }
    }
    setSaving(true);
    setStatus(null);
    try {
      const res = await authFetch('/pump/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          dest_tank: destTank,
          full_pct: pct,
          auto_enabled: autoEnabled,
          auto_start_pct: startPct,
          auto_stop_pct: stopPct,
          auto_source_tank: autoSourceTank,
          auto_source_min_pct: sourceMin,
        }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
      setDestTank(data.dest_tank || '');
      setFullPct(typeof data.full_pct === 'number' ? data.full_pct : pct);
      setAutoEnabled(!!data.auto_enabled);
      if (typeof data.auto_start_pct === 'number') setAutoStartPct(data.auto_start_pct);
      if (typeof data.auto_stop_pct === 'number') setAutoStopPct(data.auto_stop_pct);
      setAutoSourceTank(data.auto_source_tank || '');
      if (typeof data.auto_source_min_pct === 'number') setAutoSourceMin(data.auto_source_min_pct);
      setStatus({ type: 'success', message: 'Settings saved.' });
    } catch (err) {
      setStatus({ type: 'error', message: `Save failed: ${err.message}` });
    } finally {
      setSaving(false);
    }
  };

  const destName = tanks.find((t) => t.id === destTank)?.name || `tank ${destTank}`;

  return (
    <div className="pump-settings">
      <button
        type="button"
        className="pump-settings-toggle"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
      >
        ⚙ Pump settings {open ? '▲' : '▼'}
      </button>

      {open ? (
        <div className="pump-settings-body">
          {loading ? (
            <p className="pump-settings-hint">Loading…</p>
          ) : (
            <>
              <label className="pump-settings-field">
                <span>Destination tank (the tank the pump fills)</span>
                <select value={destTank} onChange={(e) => setDestTank(e.target.value)} disabled={saving}>
                  <option value="">Disabled</option>
                  {tanks.map((t) => (
                    <option key={t.id} value={t.id}>
                      {t.name || t.id}
                    </option>
                  ))}
                </select>
              </label>

              <label className="pump-settings-field">
                <span>Overflow guard — full at (%)</span>
                <input
                  type="number"
                  min="0"
                  max="100"
                  value={fullPct}
                  onChange={(e) => setFullPct(e.target.value)}
                  disabled={saving || destTank === ''}
                />
              </label>

              <p className="pump-settings-hint">
                The pump won&apos;t start — and auto-stops — when the destination reaches the level above.
                {destTank === ''
                  ? ' Interlock is disabled; only the on-device 1-minute watchdog protects the pump.'
                  : ''}
              </p>

              <div className="pump-settings-divider" />

              <label className="pump-settings-field pump-settings-checkbox">
                <input
                  type="checkbox"
                  checked={autoEnabled}
                  onChange={(e) => setAutoEnabled(e.target.checked)}
                  disabled={saving}
                />
                <span>Automatic fill (start/stop the pump on level)</span>
              </label>

              <label className="pump-settings-field">
                <span>Start when {destTank ? destName : 'destination'} at or below (%)</span>
                <input
                  type="number"
                  min="0"
                  max="100"
                  value={autoStartPct}
                  onChange={(e) => setAutoStartPct(e.target.value)}
                  disabled={saving || !autoEnabled}
                />
              </label>

              <label className="pump-settings-field">
                <span>Stop when {destTank ? destName : 'destination'} at or above (%)</span>
                <input
                  type="number"
                  min="0"
                  max="100"
                  value={autoStopPct}
                  onChange={(e) => setAutoStopPct(e.target.value)}
                  disabled={saving || !autoEnabled}
                />
              </label>

              <label className="pump-settings-field">
                <span>Source tank (pause if it runs low — prevents dry running)</span>
                <select
                  value={autoSourceTank}
                  onChange={(e) => setAutoSourceTank(e.target.value)}
                  disabled={saving || !autoEnabled}
                >
                  <option value="">No source guard</option>
                  {tanks.map((t) => (
                    <option key={t.id} value={t.id}>
                      {t.name || t.id}
                    </option>
                  ))}
                </select>
              </label>

              <label className="pump-settings-field">
                <span>Pause when source at or below (%)</span>
                <input
                  type="number"
                  min="0"
                  max="100"
                  value={autoSourceMin}
                  onChange={(e) => setAutoSourceMin(e.target.value)}
                  disabled={saving || !autoEnabled || autoSourceTank === ''}
                />
              </label>

              <p className="pump-settings-hint">
                {autoEnabled
                  ? 'The server runs the pump automatically while the destination is low and the source has water. It keeps the pump alive during a fill, so make sure the source guard is set.'
                  : 'Enable to let the server fill the destination tank automatically.'}
              </p>

              <button
                type="button"
                className="pump-button pump-button-start"
                onClick={save}
                disabled={saving}
              >
                {saving ? 'Saving…' : 'Save settings'}
              </button>

              {status ? (
                <div className={`pump-status ${status.type === 'error' ? 'is-error' : 'is-success'}`}>
                  {status.message}
                </div>
              ) : null}
            </>
          )}
        </div>
      ) : null}
    </div>
  );
}
