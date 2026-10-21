import { useMemo, useState } from 'react';
import { useAnalytics } from '../hooks/useAnalytics';
import { distanceToLevel, levelToVolume } from '../utils/calculations';
import './ChartsTab.css';

const RANGE_OPTIONS = [
  { value: '24h', label: 'Last 24 hours' },
  { value: '7d', label: 'Last 7 days' },
  { value: '30d', label: 'Last 30 days' },
];

const INTERVAL_OPTIONS = [
  { value: 'hourly', label: 'Hourly averages' },
  { value: 'raw', label: 'All readings' },
];

const METRIC_OPTIONS = [
  { value: 'levelPercent', label: 'Water Level (%)' },
  { value: 'distance', label: 'Distance (cm)' },
  { value: 'volume', label: 'Volume (L)' },
];

const CHART_DIMENSIONS = {
  width: 860,
  height: 320,
  padding: { top: 24, right: 32, bottom: 56, left: 64 },
};

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

function formatValue(metric, value) {
  if (value === undefined || value === null || Number.isNaN(value)) return '—';
  const num = Number(value);
  if (metric === 'levelPercent') return `${num.toFixed(1)}%`;
  if (metric === 'volume') return `${Math.round(num)} L`;
  return `${num.toFixed(1)} cm`;
}

export default function ChartsTab({ tank, tankConfig }) {
  const [range, setRange] = useState('7d');
  const [interval, setInterval] = useState('hourly');
  const [metric, setMetric] = useState('levelPercent');

  const { data, meta, error, loading, refresh } = useAnalytics(range, interval, tank);

  const chartData = useMemo(() => {
    return data
      .map((point) => {
        const ts = point.timestamp ? new Date(point.timestamp) : null;
        if (!ts || Number.isNaN(ts.getTime())) return null;

        const baseDistance =
          interval === 'hourly'
            ? point.avg_distance ?? point.distance ?? point.min_distance ?? point.max_distance
            : point.distance ?? point.avg_distance;

        const distance = typeof baseDistance === 'number' ? baseDistance : 0;
        const levelPercent = distanceToLevel(distance, tankConfig);
        const volume = levelToVolume(levelPercent, tankConfig);
        const minDistance = interval === 'hourly' ? point.min_distance ?? distance : distance;
        const maxDistance = interval === 'hourly' ? point.max_distance ?? distance : distance;

        return {
          timestamp: ts,
          timestampLabel: formatDateTime(point.timestamp),
          distance,
          levelPercent,
          volume,
          minDistance,
          maxDistance,
        };
      })
      .filter(Boolean)
      .sort((a, b) => a.timestamp.getTime() - b.timestamp.getTime());
  }, [data, interval, tankConfig]);

  const metricKey = metric;
  const values = useMemo(() => chartData.map((row) => row[metricKey]).filter((v) => typeof v === 'number' && !Number.isNaN(v)), [chartData, metricKey]);

  const xDomain = useMemo(() => {
    if (!chartData.length) return null;
    const minTime = chartData[0].timestamp.getTime();
    const maxTime = chartData[chartData.length - 1].timestamp.getTime();
    return { minTime, maxTime, span: Math.max(maxTime - minTime, 1) };
  }, [chartData]);

  const yDomain = useMemo(() => {
    if (!values.length) return null;

    const domainValues = (() => {
      if (metricKey !== 'distance') return values;
      const extra = chartData.flatMap((row) => [row.minDistance, row.maxDistance].filter((v) => typeof v === 'number' && !Number.isNaN(v)));
      return extra.length ? values.concat(extra) : values;
    })();

    let minValue = Math.min(...domainValues);
    let maxValue = Math.max(...domainValues);

    if (metricKey === 'levelPercent') {
      minValue = 0;
      maxValue = 100;
    } else if (metricKey === 'volume') {
      minValue = 0;
      maxValue = Math.max(100, maxValue * 1.05);
    } else {
      const padding = Math.max(1, (maxValue - minValue) * 0.1);
      minValue = Math.max(0, minValue - padding);
      maxValue = maxValue + padding;
    }

    if (minValue === maxValue) {
      maxValue += 1;
      minValue = Math.max(0, minValue - 1);
    }

    return { minValue, maxValue, span: Math.max(maxValue - minValue, 1) };
  }, [chartData, metricKey, values]);

  const chartPath = useMemo(() => {
    if (!chartData.length || !xDomain || !yDomain) return '';

    const { width, height, padding } = CHART_DIMENSIONS;
    const usableWidth = width - padding.left - padding.right;
    const usableHeight = height - padding.top - padding.bottom;

    const scaleX = (time) => padding.left + ((time - xDomain.minTime) / xDomain.span) * usableWidth;
    const scaleY = (value) =>
      padding.top + (1 - (value - yDomain.minValue) / yDomain.span) * usableHeight;

    // Build SVG path for the selected metric.
    return chartData
      .map((row, index) => {
        const x = scaleX(row.timestamp.getTime());
        const y = scaleY(row[metricKey]);
        return `${index === 0 ? 'M' : 'L'}${x},${y}`;
      })
      .join(' ');
  }, [chartData, metricKey, xDomain, yDomain]);

  const areaPath = useMemo(() => {
    if (metricKey !== 'distance' || interval !== 'hourly' || !xDomain || !yDomain || !chartData.length) {
      return '';
    }

    const { width, height, padding } = CHART_DIMENSIONS;
    const usableWidth = width - padding.left - padding.right;
    const usableHeight = height - padding.top - padding.bottom;

    const scaleX = (time) => padding.left + ((time - xDomain.minTime) / xDomain.span) * usableWidth;
    const scaleY = (value) =>
      padding.top + (1 - (value - yDomain.minValue) / yDomain.span) * usableHeight;

    // Fill the area between hourly min and max distances to visualize spread.
    const upper = chartData.map((row) => `L${scaleX(row.timestamp.getTime())},${scaleY(row.maxDistance)}`).join(' ');
    const lower = chartData
      .slice()
      .reverse()
      .map((row, index) => {
        const x = scaleX(row.timestamp.getTime());
        const y = scaleY(row.minDistance);
        return `${index === 0 ? 'L' : 'L'}${x},${y}`;
      })
      .join(' ');

    const firstX = scaleX(chartData[0].timestamp.getTime());
    const firstY = scaleY(chartData[0].maxDistance);
    return `M${firstX},${firstY} ${upper} ${lower} Z`;
  }, [chartData, interval, metricKey, xDomain, yDomain]);

  const xTicks = useMemo(() => {
    if (!xDomain) return [];
    const tickCount = 4;
    return Array.from({ length: tickCount + 1 }, (_, idx) => {
      const time = xDomain.minTime + (xDomain.span / tickCount) * idx;
      return {
        time,
        label: formatDateTime(new Date(time).toISOString()),
      };
    });
  }, [xDomain]);

  const yTicks = useMemo(() => {
    if (!yDomain) return [];
    const tickCount = 4;
    return Array.from({ length: tickCount + 1 }, (_, idx) => {
      const value = yDomain.minValue + (yDomain.span / tickCount) * idx;
      return {
        value,
        label: formatValue(metricKey, value),
      };
    });
  }, [metricKey, yDomain]);

  const summary = useMemo(() => {
    if (!values.length || !chartData.length) return null;
    const minVal = Math.min(...values);
    const maxVal = Math.max(...values);
    const latest = chartData[chartData.length - 1];
    return {
      min: minVal,
      max: maxVal,
      latestValue: latest[metricKey],
      latestTime: latest.timestampLabel,
    };
  }, [chartData, metricKey, values]);

  return (
    <div className="charts-panel">
      <div className="charts-toolbar">
        <div className="control-group">
          <label htmlFor="chart-range">Range</label>
          <select id="chart-range" value={range} onChange={(e) => setRange(e.target.value)}>
            {RANGE_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
        </div>

        <div className="control-group">
          <label htmlFor="chart-interval">Interval</label>
          <select id="chart-interval" value={interval} onChange={(e) => setInterval(e.target.value)}>
            {INTERVAL_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
        </div>

        <div className="control-group">
          <label htmlFor="chart-metric">Metric</label>
          <select id="chart-metric" value={metric} onChange={(e) => setMetric(e.target.value)}>
            {METRIC_OPTIONS.map((opt) => (
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
        <div className="charts-error">
          <strong>Could not load analytics</strong>
          <span>{error}</span>
        </div>
      )}

      {loading && !chartData.length && (
        <div className="charts-loading">
          <div className="spinner" />
          <span>Fetching timeseries…</span>
        </div>
      )}

      {!loading && !error && !chartData.length && (
        <div className="charts-empty">No data in selected window.</div>
      )}

      {!error && chartData.length > 1 && chartPath && (
        <div className="chart-wrapper">
          <svg
            className="chart-canvas"
            viewBox={`0 0 ${CHART_DIMENSIONS.width} ${CHART_DIMENSIONS.height}`}
            role="img"
            aria-label={`Timeseries for ${METRIC_OPTIONS.find((opt) => opt.value === metric)?.label ?? 'metric'}`}
          >
            <g className="chart-grid">
              {yTicks.map((tick) => (
                <line
                  key={tick.value}
                  x1={CHART_DIMENSIONS.padding.left}
                  x2={CHART_DIMENSIONS.width - CHART_DIMENSIONS.padding.right}
                  y1={(() => {
                    const { padding, height } = CHART_DIMENSIONS;
                    const usableHeight = height - padding.top - padding.bottom;
                    return (
                      padding.top +
                      (1 - (tick.value - yDomain.minValue) / yDomain.span) * usableHeight
                    );
                  })()}
                  y2={(() => {
                    const { padding, height } = CHART_DIMENSIONS;
                    const usableHeight = height - padding.top - padding.bottom;
                    return (
                      padding.top +
                      (1 - (tick.value - yDomain.minValue) / yDomain.span) * usableHeight
                    );
                  })()}
                />
              ))}

              {xTicks.map((tick) => (
                <line
                  key={tick.time}
                  x1={(() => {
                    const { padding, width } = CHART_DIMENSIONS;
                    const usableWidth = width - padding.left - padding.right;
                    return padding.left + ((tick.time - xDomain.minTime) / xDomain.span) * usableWidth;
                  })()}
                  x2={(() => {
                    const { padding, width } = CHART_DIMENSIONS;
                    const usableWidth = width - padding.left - padding.right;
                    return padding.left + ((tick.time - xDomain.minTime) / xDomain.span) * usableWidth;
                  })()}
                  y1={CHART_DIMENSIONS.padding.top}
                  y2={CHART_DIMENSIONS.height - CHART_DIMENSIONS.padding.bottom}
                />
              ))}
            </g>

            <line
              className="chart-axis"
              x1={CHART_DIMENSIONS.padding.left}
              x2={CHART_DIMENSIONS.width - CHART_DIMENSIONS.padding.right}
              y1={CHART_DIMENSIONS.height - CHART_DIMENSIONS.padding.bottom}
              y2={CHART_DIMENSIONS.height - CHART_DIMENSIONS.padding.bottom}
            />
            <line
              className="chart-axis"
              x1={CHART_DIMENSIONS.padding.left}
              x2={CHART_DIMENSIONS.padding.left}
              y1={CHART_DIMENSIONS.padding.top}
              y2={CHART_DIMENSIONS.height - CHART_DIMENSIONS.padding.bottom}
            />

            {areaPath && (
              <path className="range-band" d={areaPath} />
            )}

            <path className="chart-line" d={chartPath} />

            {chartData.map((row) => {
              const { padding, width, height } = CHART_DIMENSIONS;
              const usableWidth = width - padding.left - padding.right;
              const usableHeight = height - padding.top - padding.bottom;
              const x = padding.left + ((row.timestamp.getTime() - xDomain.minTime) / xDomain.span) * usableWidth;
              const y =
                padding.top + (1 - (row[metricKey] - yDomain.minValue) / yDomain.span) * usableHeight;
              return <circle key={row.timestamp.getTime()} className="chart-point" cx={x} cy={y} r={3} />;
            })}
          </svg>

          <div className="chart-ticks chart-ticks-x">
            {xTicks.map((tick) => (
              <span key={tick.time}>{tick.label}</span>
            ))}
          </div>

          <div className="chart-ticks chart-ticks-y">
            {yTicks.map((tick) => (
              <span key={tick.value}>{tick.label}</span>
            ))}
          </div>
        </div>
      )}

      {!error && chartData.length === 1 && (
        <div className="charts-empty">Need at least two readings to plot a chart.</div>
      )}

      {summary && (
        <div className="chart-summary">
          <div>
            <span className="summary-label">Latest</span>
            <span className="summary-value">{formatValue(metricKey, summary.latestValue)}</span>
            <span className="summary-meta">{summary.latestTime}</span>
          </div>
          <div>
            <span className="summary-label">Minimum</span>
            <span className="summary-value">{formatValue(metricKey, summary.min)}</span>
          </div>
          <div>
            <span className="summary-label">Maximum</span>
            <span className="summary-value">{formatValue(metricKey, summary.max)}</span>
          </div>
        </div>
      )}

      {meta && (
        <div className="charts-meta">
          <span>
            Showing tank {tank} with {meta.count} {meta.interval} records between {formatDateTime(meta.startTime)} and {formatDateTime(meta.endTime)}
          </span>
        </div>
      )}
    </div>
  );
}
