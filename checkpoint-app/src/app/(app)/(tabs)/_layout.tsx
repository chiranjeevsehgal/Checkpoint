import { Tabs } from 'expo-router';
import { ArrowDownUp, Bluetooth, HardDrive, Mic, Settings } from 'lucide-react-native';
import { useColorScheme } from 'nativewind';

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
          title: 'Connect',
          tabBarIcon: ({ color, size }) => <Icon as={Bluetooth} color={color} size={size} />,
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
        name="device"
        options={{
          title: 'Device',
          tabBarIcon: ({ color, size }) => <Icon as={Mic} color={color} size={size} />,
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
