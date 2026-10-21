import { formatDistance, formatTimestamp } from '../utils/calculations';
import './TankMetrics.css';

/**
 * Displays numerical metrics about the water tank.
 *
 * @param {{ distance: number, levelPercent: number, volume: number, timestamp: string }} props
 */
export default function TankMetrics({ distance, levelPercent, volume, timestamp }) {
  return (
    <div className="metrics-grid">
      <div className="metric-card">
        <span className="metric-label">Water Level</span>
        <span className="metric-value">{levelPercent}%</span>
      </div>
      <div className="metric-card">
        <span className="metric-label">Distance</span>
        <span className="metric-value">{formatDistance(distance)} <small>cm</small></span>
      </div>
      <div className="metric-card">
        <span className="metric-label">Volume (est.)</span>
        <span className="metric-value">{volume} <small>L</small></span>
      </div>
      <div className="metric-card">
        <span className="metric-label">Last Reading</span>
        <span className="metric-value time">{formatTimestamp(timestamp)}</span>
      </div>
    </div>
  );
}
