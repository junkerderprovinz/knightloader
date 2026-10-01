const fs = require('fs');
const path = require('path');
const { withDangerousMod } = require('expo/config-plugins');

/**
 * Puts back android/app/.gitkeep, the one committed file under android/:
 * F-Droid checks that the build directory exists before it runs prebuild.
 */
module.exports = function withKeptAppDir(config) {
  return withDangerousMod(config, [
    'android',
    (cfg) => {
      fs.writeFileSync(path.join(cfg.modRequest.platformProjectRoot, 'app', '.gitkeep'), '');
      return cfg;
    },
  ]);
};
