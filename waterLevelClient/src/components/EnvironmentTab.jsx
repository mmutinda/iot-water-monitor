import { useEffect, useMemo, useState } from 'react';
import {
  useEnvironmentAnalytics,
  useEnvironmentReading,
  useEnvironmentSensors,
} from '../hooks/useEnvironment';
import './EnvironmentTab.css';

const CHART = {
  width: 860,
  height: 280,
  padding: { top: 20, right: 24, bottom: 36, left: 54 },
};

function formatTimestamp(value) {
  if (!value) return 'No reading received';
  return new Date(value).toLocaleString('en-GB', {
    day: '2-digit',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

function thresholdStatus(value, low, high) {
  if (low !== null && low !== undefined && value <= low) return 'low';
  if (high !== null && high !== undefined && value >= high) return 'high';
  return 'normal';
}

function EnvironmentChart({ data, interval, metric }) {
  const points = useMemo(() => {
    const key = metric === 'temperature' ? 'temperature_c' : 'humidity_pct';
    const aggregateKey = metric === 'temperature' ? 'avg_temperature_c' : 'avg_humidity_pct';
    return data
      .map((row) => ({
        time: new Date(row.timestamp).getTime(),
        value: Number(interval === 'hourly' ? row[aggregateKey] : row[key]),
      }))
      .filter((point) => Number.isFinite(point.time) && Number.isFinite(point.value))
      .sort((a, b) => a.time - b.time);
  }, [data, interval, metric]);

  const geometry = useMemo(() => {
    if (points.length < 2) return null;
    const times = points.map((point) => point.time);
    const values = points.map((point) => point.value);
    const minTime = Math.min(...times);
    const timeSpan = Math.max(Math.max(...times) - minTime, 1);
    let minValue = Math.min(...values);
    let maxValue = Math.max(...values);
    const valuePadding = Math.max((maxValue - minValue) * 0.15, 1);
    minValue = metric === 'humidity' ? Math.max(0, minValue - valuePadding) : minValue - valuePadding;
    maxValue = metric === 'humidity' ? Math.min(100, maxValue + valuePadding) : maxValue + valuePadding;
    const valueSpan = Math.max(maxValue - minValue, 1);
    const usableWidth = CHART.width - CHART.padding.left - CHART.padding.right;
    const usableHeight = CHART.height - CHART.padding.top - CHART.padding.bottom;
    const x = (time) => CHART.padding.left + ((time - minTime) / timeSpan) * usableWidth;
    const y = (value) =>
      CHART.padding.top + (1 - (value - minValue) / valueSpan) * usableHeight;
    return {
      path: points
        .map((point, index) => `${index === 0 ? 'M' : 'L'}${x(point.time)},${y(point.value)}`)
        .join(' '),
      minValue,
      maxValue,
      firstTime: minTime,
      lastTime: Math.max(...times),
    };
  }, [metric, points]);

  if (points.length < 2 || !geometry) {
    return <div className="environment-empty">Need at least two readings to draw the chart.</div>;
  }

  const unit = metric === 'temperature' ? '°C' : '%';
  return (
    <div className="environment-chart-wrap">
      <svg
        className={`environment-chart ${metric}`}
        viewBox={`0 0 ${CHART.width} ${CHART.height}`}
        role="img"
        aria-label={`${metric} history`}
      >
        {[0, 0.25, 0.5, 0.75, 1].map((ratio) => {
          const y = CHART.padding.top +
            ratio * (CHART.height - CHART.padding.top - CHART.padding.bottom);
          return (
            <line
              key={ratio}
              className="environment-grid-line"
              x1={CHART.padding.left}
              x2={CHART.width - CHART.padding.right}
              y1={y}
              y2={y}
            />
          );
        })}
        <path className="environment-chart-line" d={geometry.path} />
      </svg>
      <div className="environment-y-labels">
        <span>{geometry.maxValue.toFixed(1)}{unit}</span>
        <span>{geometry.minValue.toFixed(1)}{unit}</span>
      </div>
      <div className="environment-x-labels">
        <span>{formatTimestamp(geometry.firstTime)}</span>
        <span>{formatTimestamp(geometry.lastTime)}</span>
      </div>
    </div>
  );
}

export default function EnvironmentTab() {
  const { sensors, loading: sensorsLoading, error: sensorsError } = useEnvironmentSensors();
  const [sensorId, setSensorId] = useState('');
  const [range, setRange] = useState('24h');
  const [metric, setMetric] = useState('temperature');

  useEffect(() => {
    if (!sensorId && sensors.length) setSensorId(sensors[0].id);
    if (sensorId && sensors.length && !sensors.some((sensor) => sensor.id === sensorId)) {
      setSensorId(sensors[0].id);
    }
  }, [sensorId, sensors]);

  const selectedSensor = sensors.find((sensor) => sensor.id === sensorId) ?? null;
  const interval = range === '24h' ? 'raw' : 'hourly';
  const { reading, loading: readingLoading, error: readingError } = useEnvironmentReading(sensorId);
  const {
    data,
    loading: analyticsLoading,
    error: analyticsError,
    refresh,
  } = useEnvironmentAnalytics(sensorId, range, interval);

  const temperatureStatus = reading
    ? thresholdStatus(
        reading.temperature_c,
        selectedSensor?.temperature_low,
        selectedSensor?.temperature_high,
      )
    : 'normal';
  const humidityStatus = reading
    ? thresholdStatus(
        reading.humidity_pct,
        selectedSensor?.humidity_low,
        selectedSensor?.humidity_high,
      )
    : 'normal';

  if (sensorsLoading) {
    return (
      <div className="environment-panel environment-centered">
        <div className="spinner" />
        <span>Loading environment sensors…</span>
      </div>
    );
  }

  if (sensorsError || !sensors.length) {
    return (
      <div className="environment-panel environment-error">
        <strong>Environment sensors unavailable</strong>
        <span>{sensorsError || 'No environment sensors are configured.'}</span>
      </div>
    );
  }

  return (
    <div className="environment-panel">
      <div className="environment-header">
        <div>
          <span className="environment-eyebrow">Indoor climate</span>
          <h2>{selectedSensor?.name || 'Environment'}</h2>
          <p>Updated {formatTimestamp(reading?.timestamp)}</p>
        </div>
        <div className="environment-sensor-select">
          <label htmlFor="environment-sensor">Sensor</label>
          <select
            id="environment-sensor"
            value={sensorId}
            onChange={(event) => setSensorId(event.target.value)}
          >
            {sensors.map((sensor) => (
              <option key={sensor.id} value={sensor.id}>{sensor.name}</option>
            ))}
          </select>
        </div>
      </div>

      {readingError && !reading ? (
        <div className="environment-error">
          <strong>Sensor has no readings</strong>
          <span>{readingError}</span>
        </div>
      ) : null}

      <div className="environment-metrics">
        <article className={`environment-metric temperature ${temperatureStatus}`}>
          <span className="environment-metric-icon" aria-hidden="true">T</span>
          <div>
            <span className="environment-metric-label">Temperature</span>
            <strong>{readingLoading && !reading ? '—' : `${reading?.temperature_c.toFixed(1) ?? '—'} °C`}</strong>
            <small>
              Alert outside {selectedSensor?.temperature_low ?? '—'}–{selectedSensor?.temperature_high ?? '—'} °C
            </small>
          </div>
        </article>
        <article className={`environment-metric humidity ${humidityStatus}`}>
          <span className="environment-metric-icon" aria-hidden="true">H</span>
          <div>
            <span className="environment-metric-label">Humidity</span>
            <strong>{readingLoading && !reading ? '—' : `${reading?.humidity_pct.toFixed(1) ?? '—'} %`}</strong>
            <small>
              Alert outside {selectedSensor?.humidity_low ?? '—'}–{selectedSensor?.humidity_high ?? '—'}%
            </small>
          </div>
        </article>
      </div>

      <section className="environment-history">
        <div className="environment-history-header">
          <div>
            <span className="environment-eyebrow">History</span>
            <h3>{metric === 'temperature' ? 'Temperature' : 'Humidity'} trend</h3>
          </div>
          <div className="environment-controls">
            <select value={metric} onChange={(event) => setMetric(event.target.value)} aria-label="Metric">
              <option value="temperature">Temperature</option>
              <option value="humidity">Humidity</option>
            </select>
            <select value={range} onChange={(event) => setRange(event.target.value)} aria-label="Range">
              <option value="24h">24 hours</option>
              <option value="7d">7 days</option>
              <option value="30d">30 days</option>
            </select>
            <button type="button" onClick={refresh} disabled={analyticsLoading}>
              {analyticsLoading ? 'Loading…' : 'Refresh'}
            </button>
          </div>
        </div>

        {analyticsError ? (
          <div className="environment-error">
            <strong>Could not load history</strong>
            <span>{analyticsError}</span>
          </div>
        ) : analyticsLoading && !data.length ? (
          <div className="environment-centered"><div className="spinner" /></div>
        ) : (
          <EnvironmentChart data={data} interval={interval} metric={metric} />
        )}
      </section>
    </div>
  );
}
