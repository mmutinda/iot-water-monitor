import { useMemo, useState } from 'react';
import { useAnalytics } from '../hooks/useAnalytics';
import { distanceToLevel, levelToVolume } from '../utils/calculations';
import './ReportsTab.css';

const RANGE_OPTIONS = [
  { value: '24h', label: 'Last 24 hours' },
  { value: '7d', label: 'Last 7 days' },
  { value: '30d', label: 'Last 30 days' },
];

const INTERVAL_OPTIONS = [
  { value: 'hourly', label: 'Hourly averages' },
  { value: 'raw', label: 'All readings' },
];

function formatDateTime(ts) {
  if (!ts) return '—';
  return new Date(ts).toLocaleString('en-GB', {
    year: 'numeric',
    month: 'short',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
}

function formatNumber(value, digits = 1) {
  if (value === undefined || value === null || Number.isNaN(value)) return '—';
  return Number(value).toFixed(digits);
}

export default function ReportsTab({ tank, tankConfig }) {
  const [range, setRange] = useState('7d');
  const [interval, setInterval] = useState('hourly');

  const { data, meta, error, loading, refresh } = useAnalytics(range, interval, tank);

  const rows = useMemo(() => {
    const sortDesc = (arr) =>
      arr.sort((a, b) => new Date(b.timestamp) - new Date(a.timestamp));

    if (interval === 'hourly') {
      return sortDesc(
        data.map((point) => {
          const levelPercent = distanceToLevel(point.avg_distance ?? 0, tankConfig);
          return {
            timestamp: point.timestamp,
            displayTime: formatDateTime(point.timestamp),
            avgDistance: formatNumber(point.avg_distance),
            minDistance: formatNumber(point.min_distance),
            maxDistance: formatNumber(point.max_distance),
            sampleCount: point.count ?? 0,
            levelPercent,
            volume: levelToVolume(levelPercent, tankConfig),
          };
        }),
      );
    }

    return sortDesc(
      data.map((point) => {
        const levelPercent = distanceToLevel(point.distance ?? 0, tankConfig);
        return {
          timestamp: point.timestamp,
          displayTime: formatDateTime(point.timestamp),
          distance: formatNumber(point.distance),
          levelPercent,
          volume: levelToVolume(levelPercent, tankConfig),
        };
      }),
    );
  }, [data, interval, tankConfig]);

  return (
    <div className="reports-panel">
      <div className="reports-toolbar">
        <div className="control-group">
          <label htmlFor="range-select">Range</label>
          <select id="range-select" value={range} onChange={(e) => setRange(e.target.value)}>
            {RANGE_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
        </div>

        <div className="control-group">
          <label htmlFor="interval-select">Interval</label>
          <select id="interval-select" value={interval} onChange={(e) => setInterval(e.target.value)}>
            {INTERVAL_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
        </div>

        <button className="refresh-button" type="button" onClick={refresh} disabled={loading}>
          {loading ? 'Refreshing…' : 'Refresh'}
        </button>
      </div>

      {error && (
        <div className="reports-error">
          <strong>Could not load analytics</strong>
          <span>{error}</span>
        </div>
      )}

      {loading && !data.length && (
        <div className="reports-loading">
          <div className="spinner" />
          <span>Fetching analytics…</span>
        </div>
      )}

      {!loading && !error && !rows.length && (
        <div className="reports-empty">No data in selected window.</div>
      )}

      {!error && rows.length > 0 && (
        <div className="reports-table-wrapper">
          <table className="reports-table">
            <thead>
              {interval === 'hourly' ? (
                <tr>
                  <th>Hour</th>
                  <th>Avg dist (cm)</th>
                  <th>Min</th>
                  <th>Max</th>
                  <th>Samples</th>
                  <th>Level (%)</th>
                  <th>Volume (L)</th>
                </tr>
              ) : (
                <tr>
                  <th>Timestamp</th>
                  <th>Distance (cm)</th>
                  <th>Level (%)</th>
                  <th>Volume (L)</th>
                </tr>
              )}
            </thead>
            <tbody>
              {rows.map((row) =>
                interval === 'hourly' ? (
                  <tr key={row.timestamp}>
                    <td>{row.displayTime}</td>
                    <td>{row.avgDistance}</td>
                    <td>{row.minDistance}</td>
                    <td>{row.maxDistance}</td>
                    <td>{row.sampleCount}</td>
                    <td>{row.levelPercent}</td>
                    <td>{row.volume}</td>
                  </tr>
                ) : (
                  <tr key={row.timestamp}>
                    <td>{row.displayTime}</td>
                    <td>{row.distance}</td>
                    <td>{row.levelPercent}</td>
                    <td>{row.volume}</td>
                  </tr>
                ),
              )}
            </tbody>
          </table>
        </div>
      )}

      {meta && (
        <div className="reports-meta">
          <span>
            Showing tank {tank} with {meta.count} {meta.interval} records between {formatDateTime(meta.startTime)} and {formatDateTime(meta.endTime)}
          </span>
        </div>
      )}
    </div>
  );
}
