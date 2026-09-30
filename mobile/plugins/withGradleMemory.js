const { withGradleProperties } = require('expo/config-plugins');

/**
 * Raises Gradle's metaspace above the template's 512 MB, which R8 runs out of
 * while it shrinks the release build.
 */
module.exports = function withGradleMemory(config) {
  return withGradleProperties(config, (cfg) => {
    const key = 'org.gradle.jvmargs';
    cfg.modResults = cfg.modResults.filter((item) => !('key' in item && item.key === key));
    cfg.modResults.push({ type: 'property', key, value: '-Xmx3072m -XX:MaxMetaspaceSize=1024m' });
    return cfg;
  });
};
