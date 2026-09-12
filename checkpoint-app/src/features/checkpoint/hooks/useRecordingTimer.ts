import { useEffect, useState } from 'react';

import { formatDuration } from '../duration.ts';

export function useRecordingTimer(recording: boolean): string {
  const [seconds, setSeconds] = useState(0);

  useEffect(() => {
    if (!recording) return;
    const startedAt = Date.now();
    const tick = () => setSeconds(Math.floor((Date.now() - startedAt) / 1000));
    const immediate = setTimeout(tick, 0);
    const timer = setInterval(tick, 1000);
    return () => {
      clearTimeout(immediate);
      clearInterval(timer);
    };
  }, [recording]);

  return formatDuration(recording ? seconds : 0);
}
