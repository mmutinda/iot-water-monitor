import { useMemo } from 'react';
import { useWaterLevel } from '../hooks/useWaterLevel';
import { distanceToLevel, getStatus, levelToVolume } from '../utils/calculations';
import WaterTank from './WaterTank';
import TankMetrics from './TankMetrics';
import StatusIndicator from './StatusIndicator';
import './LiveMonitorCard.css';

export default function LiveMonitorCard({ config, isSelected, onSelect, maxOfAllTanks }) {
  const { id: tankId } = config;
  const { data, error, loading } = useWaterLevel(tankId);

  const hasReading = Boolean(data);
  const distance = hasReading ? data.distance : null;
  const timestamp = hasReading ? data.timestamp : null;

  const levelPercent = useMemo(() => {
    if (!hasReading || distance === null || distance === undefined) {
      return 0;
    }
    return distanceToLevel(distance, config);
  }, [config, distance, hasReading]);

  const volume = useMemo(() => {
    if (!hasReading) {
      return 0;
    }
    return levelToVolume(levelPercent, config);
  }, [config, hasReading, levelPercent]);

  const status = useMemo(() => getStatus(levelPercent), [levelPercent]);

  return (
    <div className={`monitor-card ${isSelected ? 'is-selected' : ''}`}>
      <header className="monitor-card-header">
        <div>
          <h3>{config.name || `Tank ${tankId}`}</h3>
          <span className="monitor-card-sub">{config.capacityLiters.toLocaleString('en-GB')} L capacity</span>
        </div>
        <button type="button" className="monitor-card-action" onClick={onSelect} disabled={isSelected}>
          {isSelected ? 'Selected' : 'View reports'}
        </button>
      </header>

      {loading && !hasReading ? (
        <div className="monitor-card-loading">
          <div className="spinner" />
          <span>Connecting…</span>
        </div>
      ) : null}

      {error && !hasReading ? (
        <div className="monitor-card-error">
          <strong>Sensor offline</strong>
          <span>{error}</span>
        </div>
      ) : null}

      {hasReading ? (
        <div className="monitor-card-body">
          <StatusIndicator status={status} />

          <div className="monitor-card-tank">
            <WaterTank levelPercent={levelPercent} maxDistance={config.maxDistance} maxOfAllTanks={maxOfAllTanks} />
            <WaterTank levelPercent={levelPercent} maxDistance={config.maxDistance} maxOfAllTanks={maxOfAllTanks} />
          </div>

          <TankMetrics distance={distance ?? 0} levelPercent={levelPercent} volume={volume} timestamp={timestamp} />

          {error && (
            <div className="monitor-card-warning">⚠️ Connection lost — showing last reading</div>
          )}
        </div>
      ) : null}
    </div>
  );
}
