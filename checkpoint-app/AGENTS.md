# Expo HAS CHANGED

Read the exact versioned docs at https://docs.expo.dev/versions/v57.0.0/ before writing any code.

## Android native builds on Windows

`react-native-audio-api`'s CMake sources include out-of-tree `common/` files, so the default `.cxx`
staging dir under `node_modules` produces object paths over the 260-char Windows `MAX_PATH` limit and
ninja fails with `mkdir(...): No such file or directory`. The patched `android/build.gradle`
(`patches/react-native-audio-api+0.13.3.patch`) sets `cmake.buildStagingDirectory` to `C:/rnc`.
Override with `RN_AUDIO_API_CXX_DIR=<short writable path>` if needed. Delete
`node_modules/react-native-audio-api/android/.cxx` after changing it.
