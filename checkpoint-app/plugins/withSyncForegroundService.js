const { withAndroidManifest } = require('expo/config-plugins');

const SERVICE_NAME = 'app.notifee.core.ForegroundService';
const TOOLS_NAMESPACE = 'http://schemas.android.com/tools';

module.exports = function withSyncForegroundService(config) {
  return withAndroidManifest(config, (config) => {
    const manifest = config.modResults.manifest;
    if (!manifest.$['xmlns:tools']) {
      manifest.$['xmlns:tools'] = TOOLS_NAMESPACE;
    }

    const application = manifest.application?.[0];
    if (!application) return config;

    const attributes = {
      'android:name': SERVICE_NAME,
      'android:exported': 'false',
      'android:foregroundServiceType': 'connectedDevice|dataSync',
      'tools:replace': 'android:foregroundServiceType',
    };
    const services = application.service ?? [];
    const existing = services.find((service) => service.$?.['android:name'] === SERVICE_NAME);
    if (existing) {
      Object.assign(existing.$, attributes);
    } else {
      services.push({ $: attributes });
    }
    application.service = services;
    return config;
  });
};
