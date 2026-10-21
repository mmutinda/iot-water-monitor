import './TankConfigTab.css';

function formatNumber(value) {
  if (value === null || value === undefined || Number.isNaN(value)) {
    return '—';
  }
  return Number(value).toLocaleString('en-GB', { maximumFractionDigits: 2 });
}

export default function TankConfigTab({ tanks, loading, error, onRefresh }) {
  return (
    <div className="tank-config-panel">
      <div className="tank-config-toolbar">
        <div>
          <h2>Tank Configuration</h2>
          <p>Values fetched from the server are used across monitoring, reports, and charts.</p>
        </div>
        <button type="button" className="refresh-button" onClick={onRefresh} disabled={loading}>
          {loading ? 'Refreshing…' : 'Refresh'}
        </button>
      </div>

      {error && (
        <div className="tank-config-error">
          <strong>Could not load tanks</strong>
          <span>{error}</span>
        </div>
      )}

      {loading && !tanks.length && (
        <div className="tank-config-loading">
          <div className="spinner" />
          <span>Fetching tank configuration…</span>
        </div>
      )}

      {!loading && !error && !tanks.length && (
        <div className="tank-config-empty">No tanks returned by the server.</div>
      )}

      {tanks.length > 0 && (
        <div className="tank-config-table-wrapper">
          <table className="tank-config-table">
            <thead>
              <tr>
                <th>Tank</th>
                <th>Name</th>
                {/* <th>Total Height (cm)</th> */}
                <th>Sensor Offset (cm)</th>
                {/* <th>Min Distance (cm)</th> */}
                <th>Max Distance (cm)</th>
                <th>Capacity (L)</th>
              </tr>
            </thead>
            <tbody>
              {tanks.map((tank) => (
                <tr key={tank.id}>
                  <td>{tank.id}</td>
                  <td>{tank.name || tank.id}</td>
                  {/* <td>{formatNumber(tank.totalHeight)}</td> */}
                  <td>{formatNumber(tank.sensorOffset)}</td>
                  {/* <td>{formatNumber(tank.minDistance)}</td> */}
                  <td>{formatNumber(tank.maxDistance)}</td>
                  <td>{formatNumber(tank.capacityLiters)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
