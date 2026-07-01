// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

interface LogoProps {
  size?: number;
  color?: string;
}

export default function Logo({ size = 32, color = 'url(#gradient)' }: LogoProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 64 64"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
    >
      <defs>
        <linearGradient id="gradient" x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" stopColor="#14b8a6" />
          <stop offset="100%" stopColor="#0284c7" />
        </linearGradient>
      </defs>
      {/* 播放按钮背景圆 */}
      <circle cx="32" cy="32" r="28" fill={color} opacity="0.2" />
      <circle cx="32" cy="32" r="24" fill={color} opacity="0.3" />

      {/* 播放三角形 */}
      <path
        d="M24 18 L24 46 L44 32 Z"
        fill={color}
        stroke="rgba(255,255,255,0.3)"
        strokeWidth="1"
      />

      {/* 视频流线条装饰 */}
      <path
        d="M12 20 Q16 16, 20 20 T28 20"
        stroke={color}
        strokeWidth="2"
        fill="none"
        opacity="0.6"
      />
      <path
        d="M12 28 Q16 24, 20 28 T28 28"
        stroke={color}
        strokeWidth="2"
        fill="none"
        opacity="0.6"
      />
      <path
        d="M36 44 Q40 40, 44 44 T52 44"
        stroke={color}
        strokeWidth="2"
        fill="none"
        opacity="0.6"
      />
      <path
        d="M36 52 Q40 48, 44 52 T52 52"
        stroke={color}
        strokeWidth="2"
        fill="none"
        opacity="0.6"
      />
    </svg>
  );
}

