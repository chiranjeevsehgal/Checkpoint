import Svg, { Circle, Path, Rect } from 'react-native-svg';

const VIEWBOX_WIDTH = 126;
const VIEWBOX_HEIGHT = 237;

export function PendantLogo({ size = 96, color = '#ff6b57' }: { size?: number; color?: string }) {
  return (
    <Svg width={size} height={(size * VIEWBOX_HEIGHT) / VIEWBOX_WIDTH} viewBox="0 0 126 237">
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
      <Circle cx={63} cy={133} r={21} fill={color} />
    </Svg>
  );
}
