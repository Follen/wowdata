const os = require('node:os');
const path = require('node:path');

const platformAssets = {
  'win32-x64': 'wowdata-windows-amd64.exe',
  'linux-x64': 'wowdata-linux-amd64',
  'linux-arm64': 'wowdata-linux-arm64',
  'darwin-x64': 'wowdata-darwin-amd64',
  'darwin-arm64': 'wowdata-darwin-arm64'
};

function wowdataHome(env = process.env) {
  return path.resolve(env.WOWDATA_HOME || path.join(os.homedir(), '.wowdata'));
}

function agentsHome(env = process.env) {
  return path.resolve(env.AGENTS_HOME || path.join(os.homedir(), '.agents'));
}

function assetName(platform = process.platform, arch = process.arch) {
  const name = platformAssets[`${platform}-${arch}`];
  if (!name) {
    throw new Error(`unsupported platform: ${platform}-${arch}`);
  }
  return name;
}

function binaryName(platform = process.platform) {
  return platform === 'win32' ? 'wowdata.exe' : 'wowdata';
}

module.exports = { agentsHome, assetName, binaryName, platformAssets, wowdataHome };
