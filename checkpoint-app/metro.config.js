const { getDefaultConfig } = require('expo/metro-config');
const { withNativeWind } = require('nativewind/metro');

const config = getDefaultConfig(__dirname);

// Bundle the Silero VAD weights so checkSpeech works fully offline.
config.resolver.assetExts.push('onnx');

module.exports = withNativeWind(config, {
  input: './src/global.css',
  inlineRem: 16,
});
