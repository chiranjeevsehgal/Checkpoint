import { useCallback, useState } from 'react';
import { ScrollView } from 'react-native';

import { CheckpointScreen } from '../components/checkpoint-screen.tsx';
import { EnrollmentSection } from '../components/enrollment-section.tsx';
import { LedCard } from '../components/led-card.tsx';
import { ManageDeviceSheet } from '../components/manage-device-sheet.tsx';
import { PendantStatusCard } from '../components/pendant-status-card.tsx';
import { RecordingSection } from '../components/recording-section.tsx';
import { DEVICE_NAME } from '../config.ts';
import { useCheckpoint } from '../hooks/useCheckpoint.tsx';
import { linkView } from '../linkView.ts';

import { AppHeader } from '@/components/shared/app-header';
import { AppRefreshControl } from '@/components/ui/refresh-control';
import { useRefresh } from '@/lib/use-refresh';

export function ConnectScreen() {
  const { connected, deviceName, linkState, refreshStatus, refreshStorage } = useCheckpoint();
  const [setupOpen, setSetupOpen] = useState(false);
  const [sheetOpen, setSheetOpen] = useState(false);

  const view = linkView(linkState, deviceName.trim() || DEVICE_NAME);

  const refresh = useCallback(async () => {
    if (!connected) return;
    await refreshStatus();
    await refreshStorage();
  }, [connected, refreshStatus, refreshStorage]);
  const { refreshing, onRefresh } = useRefresh(refresh);

  return (
    <CheckpointScreen>
      <AppHeader title="Pendant" subtitle={view.label} />
      <ScrollView
        className="flex-1"
        nestedScrollEnabled
        keyboardShouldPersistTaps="handled"
        refreshControl={<AppRefreshControl refreshing={refreshing} onRefresh={onRefresh} />}
        contentContainerStyle={{ gap: 24, paddingBottom: 24 }}
        showsVerticalScrollIndicator={false}
      >
        <PendantStatusCard onManage={() => setSheetOpen(true)} />
        <RecordingSection />
        <LedCard />
        <EnrollmentSection forceOpen={setupOpen} />
      </ScrollView>

      <ManageDeviceSheet
        visible={sheetOpen}
        onClose={() => setSheetOpen(false)}
        onSetup={() => setSetupOpen(true)}
      />
    </CheckpointScreen>
  );
}
