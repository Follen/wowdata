const path = require('path');
const fs = require('fs');

// Polyfill Symbol.dispose if not present
if (!Symbol.dispose) {
    Symbol.dispose = Symbol('dispose');
}

// Mock NW.js global object
global.nw = {
    App: {
        dataPath: path.join(__dirname, '../user_data'),
        manifest: {
            version: '0.0.0'
        },
        startPath: __dirname
    },
    Shell: {
        openItem: (item) => console.log('Opening item:', item)
    },
    Window: {
        get: () => ({
            on: () => { },
            show: () => { },
            hide: () => { },
            close: () => { },
            resizeTo: () => { },
            setResizable: () => { },
            setMaximumSize: () => { },
            setMinimumSize: () => { },
            setPosition: () => { },
            minimize: () => { },
            toggleFullscreen: () => { },
            setAlwaysOnTop: () => { },
            showDevTools: () => { }
        })
    }
};

// Mock DOM objects for Node.js
global.window = {
    localStorage: {
        getItem: () => null,
        setItem: () => { },
        removeItem: () => { }
    },
    dispatchEvent: () => { },
    addEventListener: () => { }
};

global.document = {
    getElementById: () => ({ value: '', checked: false }),
    createElement: () => ({ style: {}, classList: { add: () => { } } }),
    addEventListener: () => { }
};

global.navigator = {
    userAgent: 'Node.js'
};

global.requestAnimationFrame = (cb) => setTimeout(cb, 16);

// Ensure user data directory exists
if (!fs.existsSync(global.nw.App.dataPath)) {
    fs.mkdirSync(global.nw.App.dataPath, { recursive: true });
}

// Emulate BUILD_RELEASE flag
global.BUILD_RELEASE = false;
