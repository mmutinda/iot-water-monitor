import './StatusIndicator.css';

/**
 * Color-coded status badge indicating tank level condition.
 *
 * @param {{ status: { label: string, color: string, bgColor: string } }} props
 */
export default function StatusIndicator({ status }) {
  return (
    <div
      className="status-badge"
      style={{ color: status.color, backgroundColor: status.bgColor, borderColor: status.color }}
    >
      <span className="status-dot" style={{ backgroundColor: status.color }} />
      {status.label}
    </div>
  );
}
