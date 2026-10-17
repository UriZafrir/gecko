// electron-builder config for the Gecko desktop apps, run by build.sh:
// a universal macOS dmg, a Linux AppImage and a Windows setup.exe (plus a
// portable zip). Each carries the gecko binary for its OS and arch, which
// build.sh puts in dist/build/desktop/<os>-<arch>/.
const path = require('node:path');

const root = path.resolve(__dirname, '..', '..');
const bins = path.join(root, 'dist', 'build', 'desktop');

module.exports = {
  appId: 'dev.gecko.app',
  productName: 'Gecko',
  copyright: 'Gecko contributors',
  electronVersion: require(path.join(root, 'desktop', 'package.json')).devDependencies.electron,
  directories: { output: path.join(root, 'dist', 'desktop') },
  files: ['main.js', 'preload.js', 'icon.png', 'build-id.json', 'package.json'],
  // Nothing to update from and nothing to publish to.
  publish: null,
  npmRebuild: false,
  afterPack: path.join(__dirname, 'after-pack.cjs'),
  artifactName: 'Gecko-${version}-${os}-${arch}.${ext}',

  mac: {
    target: [{ target: 'dmg', arch: ['universal'] }],
    icon: path.join(root, 'packaging', 'macos', 'gecko.icns'),
    category: 'public.app-category.developer-tools',
    // Unsigned: ad-hoc signature only, which Apple silicon needs to run it.
    identity: '-',
    hardenedRuntime: false,
    gatekeeperAssess: false,
    minimumSystemVersion: '12.0',
    extraResources: [{ from: path.join(bins, 'darwin-${arch}', 'gecko'), to: 'gecko' }],
    extendInfo: { LSApplicationCategoryType: 'public.app-category.developer-tools' },
  },
  dmg: {
    sign: false,
    artifactName: 'Gecko-${version}-mac-universal.${ext}',
  },

  linux: {
    target: [{ target: 'AppImage', arch: ['x64'] }],
    executableName: 'gecko-desktop',
    icon: path.join(root, 'packaging', 'linux', 'gecko-256.png'),
    category: 'System;TerminalEmulator',
    synopsis: 'Terminal multiplexer for coding agents',
    description: 'Gecko keeps terminal sessions alive in a daemon and shows them in a native, see-through window.',
    extraFiles: [{ from: path.join(bins, 'linux-${arch}', 'gecko'), to: 'usr/bin/gecko' }],
    desktop: { entry: { StartupWMClass: 'gecko', Name: 'Gecko', Comment: 'Terminal multiplexer for coding agents' } },
  },
  // The static AppImage runtime: no libfuse2 needed (Ubuntu 22.04+ and
  // Fedora don't install it).
  toolsets: { appimage: '1.0.3' },
  appImage: {
    // AppRun adds --no-sandbox itself only where the sandbox can't start.
    executableArgs: [],
  },

  win: {
    target: [
      { target: 'nsis', arch: ['x64'] },
      { target: 'zip', arch: ['x64'] },
    ],
    icon: path.join(root, 'packaging', 'windows', 'gecko.ico'),
    extraResources: [{ from: path.join(bins, 'windows-${arch}', 'gecko.exe'), to: 'gecko.exe' }],
    signAndEditExecutable: true,
  },
  nsis: {
    oneClick: false,
    perMachine: false,
    allowToChangeInstallationDirectory: true,
    artifactName: 'Gecko-${version}-windows-${arch}-setup.${ext}',
    shortcutName: 'Gecko',
  },
};
