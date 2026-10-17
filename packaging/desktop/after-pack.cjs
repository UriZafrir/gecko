// electron-builder afterPack hook. Linux: the AppImage's AppRun starts
// "gecko-desktop", which becomes gecko-desktop.sh (CLI dispatch, sandbox
// note, a gecko copy that outlives the mount) in front of the real Electron
// binary, gecko-desktop.bin.
const fs = require('node:fs');
const path = require('node:path');

exports.default = async function afterPack(ctx) {
  if (ctx.electronPlatformName !== 'linux') return;
  const exe = path.join(ctx.appOutDir, ctx.packager.executableName);
  fs.renameSync(exe, exe + '.bin');
  fs.copyFileSync(path.join(__dirname, 'gecko-desktop.sh'), exe);
  fs.chmodSync(exe, 0o755);
};
