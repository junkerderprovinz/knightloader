import { AppRegistry } from 'react-native';
import { registerRootComponent } from 'expo';

import App from './App';
import { WATCH_TASK, watchTask } from './src/watch/watch';

// registerRootComponent calls AppRegistry.registerComponent('main', () => App);
// It also ensures that whether you load the app in Expo Go or in a native build,
// the environment is set up appropriately
registerRootComponent(App);

// The background watch's pass, which modules/watch's service runs as a
// headless task. Registered here, beside the app, because the service may
// bring up the JavaScript runtime without the app's screen.
AppRegistry.registerHeadlessTask(WATCH_TASK, () => watchTask);
