const fs = require('fs');
const path = require('path');

if (!Symbol.dispose)
	Symbol.dispose = Symbol('dispose');

global.BUILD_RELEASE = true;
globalThis.requestAnimationFrame = (cb) => setImmediate(cb);

const dataPath = path.join(__dirname, '..', 'user_data');
fs.mkdirSync(dataPath, { recursive: true });

global.nw = {
	App: {
		dataPath,
		manifest: { version: '1.0.0' },
		startPath: path.join(__dirname, '..')
	}
};

global.navigator = { userAgent: 'Node.js' };
