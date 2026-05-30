const path = require('path');

const DATA_PATH = path.join(__dirname, '..', 'user_data');

module.exports = {
	INSTALL_PATH: path.join(__dirname, '..'),
	DATA_PATH,
	RUNTIME_LOG: path.join(DATA_PATH, 'runtime.log'),
	LAST_EXPORT: path.join(DATA_PATH, 'last_export'),
	LISTFILE_MODEL_FILTER: /(_\d\d\d_)|(_\d\d\d.wmo$)|(lod\d.wmo$)/,
	USER_AGENT: 'wow-mcp',
	GAME: {
		MAP_SIZE: 64,
		MAP_SIZE_SQ: 4096,
		TILE_SIZE: 533.3333333333333,
		MAP_OFFSET: 17066.666666666668
	},
	CACHE: {
		DIR: path.join(DATA_PATH, 'casc'),
		SIZE: path.join(DATA_PATH, 'casc', 'cachesize'),
		INTEGRITY_FILE: path.join(DATA_PATH, 'casc', 'cacheintegrity'),
		SIZE_UPDATE_DELAY: 5000,
		DIR_BUILDS: path.join(DATA_PATH, 'casc', 'builds'),
		DIR_INDEXES: path.join(DATA_PATH, 'casc', 'indices'),
		DIR_DATA: path.join(DATA_PATH, 'casc', 'data'),
		DIR_DBD: path.join(DATA_PATH, 'casc', 'dbd'),
		DIR_LISTFILE: path.join(DATA_PATH, 'casc', 'listfile'),
		BUILD_MANIFEST: 'manifest.json',
		BUILD_LISTFILE: 'listfile',
		BUILD_ENCODING: 'encoding',
		BUILD_ROOT: 'root',
		LISTFILE_DATA: 'listfile.txt',
		TACT_KEYS: path.join(DATA_PATH, 'tact.json'),
		REALMLIST: path.join(DATA_PATH, 'realmlist.json')
	},
	PRODUCTS: [
		{ product: 'wow', title: 'World of Warcraft', tag: 'Retail' },
		{ product: 'wowt', title: 'PTR: World of Warcraft', tag: 'PTR' },
		{ product: 'wowxptr', title: 'Beta: World of Warcraft', tag: 'Beta' },
		{ product: 'wow_classic', title: 'World of Warcraft Classic', tag: 'Classic' },
		{ product: 'wow_classic_titan', title: 'World of Warcraft Classic Titan Reforged', tag: 'Titan Reforged' },
		{ product: 'wow_classic_era', title: 'World of Warcraft Classic Era', tag: 'Classic Era' }
	],
	PATCH: {
		REGIONS: [
			{ tag: 'eu', name: 'Europe' },
			{ tag: 'us', name: 'Americas' },
			{ tag: 'kr', name: 'Korea' },
			{ tag: 'tw', name: 'Taiwan' },
			{ tag: 'cn', name: 'China' }
		],
		DEFAULT_REGION: 'us',
		HOST: 'https://%s.version.battle.net/',
		HOST_CHINA: 'https://cn.version.battlenet.com.cn/',
		VERSION_CONFIG: '/versions',
		SERVER_CONFIG: '/cdns',
		ENDPOINT: '%s/versions',
		CDNS: '%s/cdns',
		MANIFEST: '.build.info',
		DATA_DIR: 'Data'
	}
};
