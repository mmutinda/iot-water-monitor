import { useId, useMemo } from 'react';
import { getWaterColor } from '../utils/calculations';
import './WaterTank.css';

/**
 * SVG-based water tank visualization.
 * Renders a realistic tank with animated water fill and wave effect.
 *
 * @param {{ levelPercent: number }} props
 */
export default function WaterTank({ levelPercent, maxDistance = 1, maxOfAllTanks = 1 }) {
  const instanceId = useId().replace(/:/g, '');
  const colors = useMemo(() => getWaterColor(levelPercent), [levelPercent]);
  const waterGradId = `waterGrad-${instanceId}`;
  const tankClipId = `tankClip-${instanceId}`;
  const wavePatternId = `wavePattern-${instanceId}`;

  // Scale tank height proportionally to this tank's max_distance
  const heightScale = maxOfAllTanks > 0 ? maxDistance / maxOfAllTanks : 1;

  // SVG viewBox dimensions
  const vbW = 200;
  const tankX = 30;
  const tankY = 40;
  const tankW = 140;
  const tankH = Math.round(260 * heightScale);
  const vbH = tankY + tankH + 40; // 40px bottom margin for base + stand legs
  const tankR = 8; // corner radius

  // Water dimensions
  const waterPadding = 3;
  const waterX = tankX + waterPadding;
  const waterW = tankW - waterPadding * 2;
  const waterMaxH = tankH - waterPadding * 2;
  const waterH = (levelPercent / 100) * waterMaxH;
  const waterY = tankY + tankH - waterPadding - waterH;

  // Graduation marks
  const marks = [0, 25, 50, 75, 100];

  return (
    <div className="tank-container">
      <svg
        viewBox={`0 0 ${vbW} ${vbH}`}
        className="tank-svg"
        xmlns="http://www.w3.org/2000/svg"
      >
        <defs>
          {/* Water gradient */}
          <linearGradient id={waterGradId} x1="0" x2="0" y1="0" y2="1">
            <stop offset="0%" stopColor={colors.wave} stopOpacity="0.9" />
            <stop offset="100%" stopColor={colors.fill} stopOpacity="1" />
          </linearGradient>

          {/* Tank metallic gradient */}
          <linearGradient id="tankGrad" x1="0" x2="1" y1="0" y2="0">
            <stop offset="0%" stopColor="#555" />
            <stop offset="30%" stopColor="#888" />
            <stop offset="50%" stopColor="#aaa" />
            <stop offset="70%" stopColor="#888" />
            <stop offset="100%" stopColor="#555" />
          </linearGradient>

          {/* Clip path for water inside tank */}
          <clipPath id={tankClipId}>
            <rect
              x={waterX}
              y={tankY + waterPadding}
              width={waterW}
              height={waterMaxH}
              rx={tankR - waterPadding}
              ry={tankR - waterPadding}
            />
          </clipPath>

          {/* Wave pattern */}
          <pattern
            id={wavePatternId}
            x="0"
            y="0"
            width="80"
            height="12"
            patternUnits="userSpaceOnUse"
          >
            <path
              d="M0 6 Q10 0 20 6 Q30 12 40 6 Q50 0 60 6 Q70 12 80 6"
              fill="none"
              stroke={colors.wave}
              strokeWidth="2.5"
              opacity="0.5"
            />
          </pattern>
        </defs>

        {/* === Sensor at top === */}
        <rect x={vbW / 2 - 14} y={20} width={28} height={18} rx={3} fill="#444" stroke="#666" strokeWidth="1" />
        <circle cx={vbW / 2 - 5} cy={29} r={3} fill="#222" stroke="#555" strokeWidth="0.5" />
        <circle cx={vbW / 2 + 5} cy={29} r={3} fill="#222" stroke="#555" strokeWidth="0.5" />
        {/* Sensor mount legs */}
        <line x1={vbW / 2 - 10} y1={38} x2={vbW / 2 - 10} y2={tankY} stroke="#666" strokeWidth="1.5" />
        <line x1={vbW / 2 + 10} y1={38} x2={vbW / 2 + 10} y2={tankY} stroke="#666" strokeWidth="1.5" />

        {/* === Tank body === */}
        <rect
          x={tankX}
          y={tankY}
          width={tankW}
          height={tankH}
          rx={tankR}
          ry={tankR}
          fill="none"
          stroke="url(#tankGrad)"
          strokeWidth="4"
        />

        {/* Tank inner shadow */}
        <rect
          x={tankX + 2}
          y={tankY + 2}
          width={tankW - 4}
          height={tankH - 4}
          rx={tankR - 1}
          ry={tankR - 1}
          fill="#1a1a2e"
          opacity="0.4"
        />

        {/* === Water fill === */}
        <g clipPath={`url(#${tankClipId})`}>
          {/* Main water body */}
          <rect
            className="water-body"
            x={waterX}
            y={waterY}
            width={waterW}
            height={waterH + 6}
            fill={`url(#${waterGradId})`}
          />

          {/* Animated wave on water surface */}
          {levelPercent > 0 && (
            <rect
              className="water-wave"
              x={waterX}
              y={waterY - 6}
              width={waterW}
              height={12}
              fill={`url(#${wavePatternId})`}
            />
          )}

          {/* Water shine/reflection */}
          <rect
            x={waterX + 8}
            y={waterY + 10}
            width={6}
            height={Math.min(waterH * 0.6, 80)}
            rx={3}
            fill="rgba(255,255,255,0.12)"
          />
        </g>

        {/* === Graduation marks === */}
        {marks.map((pct) => {
          const markY = tankY + tankH - waterPadding - (pct / 100) * waterMaxH;
          return (
            <g key={pct}>
              <line
                x1={tankX + tankW + 4}
                y1={markY}
                x2={tankX + tankW + 14}
                y2={markY}
                stroke="#888"
                strokeWidth="1"
              />
              <text
                x={tankX + tankW + 18}
                y={markY + 4}
                fill="#999"
                fontSize="10"
                fontFamily="monospace"
              >
                {pct}%
              </text>
            </g>
          );
        })}

        {/* === Level indicator line === */}
        {levelPercent > 0 && (
          <line
            className="level-line"
            x1={tankX - 10}
            y1={waterY}
            x2={tankX + 4}
            y2={waterY}
            stroke={colors.fill}
            strokeWidth="2"
            strokeDasharray="3,2"
          />
        )}

        {/* === Tank base/stand === */}
        <rect
          x={tankX - 8}
          y={tankY + tankH + 2}
          width={tankW + 16}
          height={8}
          rx={2}
          fill="#555"
          stroke="#666"
          strokeWidth="1"
        />
        {/* Stand legs */}
        <rect x={tankX} y={tankY + tankH + 10} width={8} height={14} rx={1} fill="#444" />
        <rect x={tankX + tankW - 8} y={tankY + tankH + 10} width={8} height={14} rx={1} fill="#444" />
      </svg>
    </div>
  );
}
