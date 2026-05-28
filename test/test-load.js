BUILD_RELEASE = true;
globalThis.requestAnimationFrame = (cb) => setImmediate(cb);
const fs = require('fs');
const LOG = __dirname + '/test.log';
fs.writeFileSync(LOG, '');
const log = (msg) => fs.appendFileSync(LOG, new Date().toISOString() + ' ' + msg + '\n');

process.on('uncaughtException', (e) => { log('UNCAUGHT: ' + e.message + '\n' + e.stack); process.exit(1); });
process.on('unhandledRejection', (e) => { log('UNHANDLED: ' + (e?.message||e) + '\n' + (e?.stack||'')); process.exit(1); });

log('START');
require('./core/shim');
const core = require('./core/core');
const LocaleFlag = require('./core/casc/locale-flags').flags;
const CASCLocal = require('./core/casc/casc-source-local');
log('MODULES OK');

core.view = core.makeNewView();
core.view.$watch = (p,cb,o) => { if(o&&o.immediate){if(p==='config.cascLocale')cb(LocaleFlag.zhCN);else cb(undefined);} return ()=>{}; };
try{core.view.config=require('./core/config').getDefaults();}catch(e){core.view.config={};}
core.view.selectedCDNRegion = { tag: 'cn' };

// Patch out redraw to avoid requestAnimationFrame issues
core.progressLoadingScreen = async (text) => { log('STEP: ' + text); };

const src = new CASCLocal('D:/Game/World of Warcraft');
(async()=>{
  await src.init();
  log('CONNECT OK');
  try { await src.load(0); log('LOAD OK'); }
  catch(e) { log('LOAD ERR: ' + e.message + '\n' + e.stack); }
  log('DONE');
  setTimeout(() => process.exit(0), 200);
})();
