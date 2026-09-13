import { Tabs } from 'expo-router';
import { ArrowDownUp, HardDrive, Settings } from 'lucide-react-native';
import { useColorScheme } from 'nativewind';

import { PendantLogo } from '@/components/shared/pendant-logo';
import { Icon } from '@/components/ui/icon';
import { PALETTE } from '@/lib/theme';

export default function TabsLayout() {
  const { colorScheme } = useColorScheme();
  const palette = colorScheme === 'dark' ? PALETTE.dark : PALETTE.light;

  return (
    <Tabs
      screenOptions={{
        headerShown: false,
        tabBarActiveTintColor: palette.primary,
        tabBarInactiveTintColor: palette.mutedForeground,
        tabBarStyle: {
          backgroundColor: palette.background,
          borderTopColor: palette.divider,
          borderTopWidth: 2,
        },
        tabBarLabelStyle: {
          fontFamily: 'Archivo_600SemiBold',
          fontSize: 10,
        },
      }}
    >
      <Tabs.Screen
        name="connect"
        options={{
          title: 'Pendant',
          tabBarIcon: ({ color, size }) => <PendantLogo height={size} color={color} />,
        }}
      />
      <Tabs.Screen
        name="transfers"
        options={{
          title: 'Transfers',
          tabBarIcon: ({ color, size }) => <Icon as={ArrowDownUp} color={color} size={size} />,
        }}
      />
      <Tabs.Screen
        name="storage"
        options={{
          title: 'Storage',
          tabBarIcon: ({ color, size }) => <Icon as={HardDrive} color={color} size={size} />,
        }}
      />
      <Tabs.Screen
        name="settings"
        options={{
          title: 'Settings',
          tabBarIcon: ({ color, size }) => <Icon as={Settings} color={color} size={size} />,
        }}
      />
    </Tabs>
  );
}
