import type { ColorValue } from 'react-native';
import Svg, { Circle, Path, Rect } from 'react-native-svg';

const VIEWBOX_WIDTH = 126;
const VIEWBOX_HEIGHT = 237;

interface PendantLogoProps {
  size?: number;
  height?: number;
  color?: ColorValue;
  dotColor?: ColorValue;
}

export function PendantLogo({
  size = 96,
  height,
  color = '#ff6b57',
  dotColor = color,
}: PendantLogoProps) {
  const width = height ? (height * VIEWBOX_WIDTH) / VIEWBOX_HEIGHT : size;
  const renderedHeight = height ?? (size * VIEWBOX_HEIGHT) / VIEWBOX_WIDTH;

  return (
    <Svg width={width} height={renderedHeight} viewBox="0 0 126 237">
      <Path fill={color} d="M54 0h18v45H54z" />
      <Rect
        x={9}
        y={46}
        width={108}
        height={182}
        rx={54}
        fill="none"
        stroke={color}
        strokeWidth={18}
      />
      <Circle cx={63} cy={133} r={21} fill={dotColor} />
    </Svg>
  );
}
