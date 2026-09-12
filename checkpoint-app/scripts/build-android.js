#!/usr/bin/env node

const { spawnSync } = require('child_process');
const path = require('path');
const fs = require('fs');

const androidDir = path.join(__dirname, '..', 'android');
const gradlew = process.platform === 'win32' ? 'gradlew.bat' : './gradlew';
const args = ['assembleRelease', ...process.argv.slice(2)];

if (!fs.existsSync(path.join(androidDir, gradlew))) {
  console.error('android/ is missing. Run `npx expo prebuild -p android` first.');
  process.exit(1);
}

const result = spawnSync(gradlew, args, {
  cwd: androidDir,
  stdio: 'inherit',
  shell: process.platform === 'win32',
  env: { ...process.env, NODE_ENV: 'production' },
});

if (result.error) {
  console.error(result.error.message);
  process.exit(1);
}

process.exit(result.status ?? 1);
