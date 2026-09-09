import { Tabs } from 'expo-router';
import { House, Search, Settings } from 'lucide-react-native';

import { Icon } from '@/components/ui/icon';

export default function TabsLayout() {
  return (
    <Tabs screenOptions={{ headerShown: false }}>
      <Tabs.Screen
        name="index"
        options={{
          title: 'Home',
          tabBarIcon: ({ color, size }) => <Icon as={House} color={color} size={size} />,
        }}
      />
      <Tabs.Screen
        name="search"
        options={{
          title: 'Search',
          tabBarIcon: ({ color, size }) => <Icon as={Search} color={color} size={size} />,
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
