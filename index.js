BUILD_RELEASE = true;
console.log = console.error;
globalThis.requestAnimationFrame = (cb) => setImmediate(cb);

const path = require('path');
const fs = require('fs');

require('./core/shim');

const core = require('./core/core');
const constants = require('./core/constants');
const LocaleFlag = require('./core/casc/locale-flags').flags;
const CASCRemote = require('./core/casc/casc-source-remote');
const CASCLocal = require('./core/casc/casc-source-local');
const BLPFile = require('./core/casc/blp');
const db2 = require('./core/casc/db2');
const listfile = require('./core/casc/listfile');
const dbdManifest = require('./core/casc/dbd-manifest');

let cascSource = null;
let isCascReady = false;
let currentRegion = 'cn';
let autoWarmupPromise = null;
let autoWarmupError = null;

const OUTPUT_DIR = path.join(__dirname, 'output');
if (!fs.existsSync(OUTPUT_DIR)) fs.mkdirSync(OUTPUT_DIR, { recursive: true });

const DEFAULT_WARMUP_TABLES = [
	'SpellName',
	'Spell',
	'SpellEffect',
	'SpellMisc',
	'SpellCastTimes',
	'SpellDuration',
	'SpellRange',
	'JournalEncounterSection'
];

const getLocaleForRegion = (region) => {
	switch (region) {
		case 'cn': return LocaleFlag.zhCN;
		case 'tw': return LocaleFlag.zhTW;
		case 'kr': return LocaleFlag.koKR;
		default: return LocaleFlag.enUS;
	}
};

core.view.$watch = (property, callback, options) => {
	if (options && options.immediate)
		callback(property === 'config.cascLocale' ? getLocaleForRegion(currentRegion) : undefined);
	return () => {};
};

core.view.config = {
	enableBinaryListfile: false,
	listfileBinarySource: 'https://www.kruithne.net/wow.export/data/listfile/%s',
	listfileURL: 'https://github.com/wowdev/wow-listfile/releases/latest/download/community-listfile.csv',
	listfileFallbackURL: 'https://www.kruithne.net/wow.export/data/listfile/master',
	listfileCacheRefresh: 30,
	listfileSortByID: false,
	dbdURL: 'https://raw.githubusercontent.com/wowdev/WoWDBDefs/refs/heads/master/definitions/%s.dbd',
	dbdFallbackURL: 'https://www.kruithne.net/wow.export/data/dbd/?def=%s',
	dbdFilenameURL: 'https://raw.githubusercontent.com/wowdev/WoWDBDefs/refs/heads/master/manifest.json',
	dbdFilenameFallbackURL: 'https://www.kruithne.net/wow.export/data/dbd',
	tactKeysURL: 'https://raw.githubusercontent.com/wowdev/TACTKeys/master/WoW.txt',
	tactKeysFallbackURL: 'https://www.kruithne.net/wow.export/data/tact/wow',
	cascLocale: 2,
	enableUnknownFiles: false,
	cacheExpiry: 7,
	exportDirectory: '',
	pathFormat: 'win32',
	modelsShowM2: true,
	modelsShowM3: true,
	modelsShowWMO: true,
	removePathSpaces: false
};

core.progressLoadingScreen = async (text) => {
	core.view.loadingProgress = text;
	console.error('[wow-mcp] ' + text);
};

function textResult(data) {
	return { content: [{ type: 'text', text: typeof data === 'string' ? data : JSON.stringify(data, null, 2) }] };
}

function errorResult(error) {
	return { content: [{ type: 'text', text: 'Error: ' + error.message }], isError: true };
}

function requireReady() {
	if (!isCascReady)
		throw new Error('CASC 未就绪，请先调用 wow_warmup');
}

async function ensureReady() {
	if (autoWarmupPromise && !isCascReady)
		await autoWarmupPromise;
	if (autoWarmupError)
		throw autoWarmupError;
	requireReady();
}

async function ensureListfileLoaded() {
	if (!cascSource)
		throw new Error('请先调用 wow_warmup');
	requireReady();
	if (listfile.isLoaded())
		return;

	await cascSource.prepareListfile();
	await cascSource.loadListfile(cascSource.getBuildKey());
}

function parseArgs() {
	const args = process.argv.slice(2);
	const opts = {};
	for (let i = 0; i < args.length; i++) {
		if (args[i] === '--local' && args[i + 1]) opts.source = 'local', opts.path = args[++i];
		else if (args[i] === '--remote') opts.source = 'remote', opts.region = args[++i] || 'cn';
		else if (args[i] === '--product' && args[i + 1]) opts.product = args[++i];
		else if (args[i] === '--build' && args[i + 1]) opts.buildIndex = parseInt(args[++i], 10);
		else if (args[i] === '--region' && args[i + 1]) opts.region = args[++i];
	}
	return opts;
}

async function connectSource(options) {
	currentRegion = options.region || currentRegion || 'cn';
	if (options.source === 'local') {
		if (!options.path)
			throw new Error('source=local 需要 path');
		core.view.selectedCDNRegion = { tag: currentRegion };
		cascSource = new CASCLocal(options.path);
	} else {
		cascSource = new CASCRemote(currentRegion);
	}

	isCascReady = false;
	await cascSource.init();
	return cascSource.getProductList();
}

function getWarmupPrompt(extra = {}) {
	return {
		action: 'choose_warmup_target',
		message: '请选择要预热的 WoW 数据源和客户端版本。预热可能需要较久，模型应耐心等待，建议工具超时 240 秒。',
		sources: [
			{ source: 'local', label: '本地客户端', required: ['path'], optional: ['region'] },
			{ source: 'remote', label: '远端 CDN', required: ['region'], optional: [] }
		],
		regions: [
			{ region: 'cn', label: '中国' },
			{ region: 'us', label: '美洲' },
			{ region: 'eu', label: '欧洲' },
			{ region: 'kr', label: '韩国' },
			{ region: 'tw', label: '台湾' }
		],
		productHints: [
			{ product: 'wow', label: 'Retail' },
			{ product: 'wow_classic', label: 'Classic' },
			{ product: 'wow_classic_titan', label: 'Titan Reforged' },
			{ product: 'wow_classic_era', label: 'Classic Era' },
			{ product: 'wowt', label: 'PTR' },
			{ product: 'wowxptr', label: 'Beta' }
		],
		connected: Boolean(cascSource),
		products: cascSource ? cascSource.getProductList() : [],
		examples: [
			{ source: 'remote', region: 'cn' },
			{ source: 'remote', region: 'cn', product: 'wow' },
			{ source: 'local', path: 'D:/World of Warcraft/_retail_', region: 'cn', product: 'wow' }
		],
		...extra
	};
}

function findBuildIndex(products, product, buildIndex) {
	if (buildIndex !== undefined)
		return buildIndex;
	if (!product)
		return undefined;

	const productMeta = constants.PRODUCTS.find(p => p.product === product || p.tag === product);
	if (productMeta?.tag) {
		const byTag = products.find(p => String(p.label || '').toLowerCase().includes(productMeta.tag.toLowerCase()));
		if (byTag)
			return byTag.buildIndex;
	}

	const needles = [productMeta?.title, product].filter(Boolean).map(s => String(s).toLowerCase());
	const match = products.find(p => needles.some(needle => String(p.label || '').toLowerCase().includes(needle)));
	return match ? match.buildIndex : undefined;
}

async function runWarmup(options = {}) {
	const timings = [];
	const mark = async (step, fn) => {
		const start = Date.now();
		const result = await fn();
		timings.push({ step, ms: Date.now() - start });
		return result;
	};

	if (options.source)
		await mark('connect', () => connectSource(options));
	else if (!cascSource)
		return getWarmupPrompt();

	const products = cascSource.getProductList();
	const buildIndex = findBuildIndex(products, options.product, options.buildIndex);
	if (buildIndex === undefined)
		return getWarmupPrompt({ products, missing: ['buildIndex'], hint: '请选择 products 里的 buildIndex，或传 product: wow/wow_classic/wow_classic_titan/wow_classic_era/wowt/wowxptr。' });

	if (!isCascReady || options.forceReload) {
		await mark('loadBuild', () => cascSource.load(buildIndex));
		isCascReady = true;
	}

	const warm = options.warm || {};
	const warmListfile = warm.listfile !== false;
	const warmDbdManifest = warm.dbdManifest !== false;
	const tables = Array.isArray(warm.tables) ? warm.tables : DEFAULT_WARMUP_TABLES;

	if (warmDbdManifest)
		await mark('dbdManifest', () => dbdManifest.prepareManifest());
	if (warmListfile)
		await mark('listfile', () => ensureListfileLoaded());
	for (const table of tables)
		await mark(`table:${table}`, () => db2.preload[table]());

	return {
		success: true,
		message: 'warmup 完成',
		region: currentRegion,
		buildIndex,
		buildName: cascSource.getBuildName(),
		warmed: { listfile: warmListfile, dbdManifest: warmDbdManifest, tables },
		timings,
		note: '首次预热可能接近 240 秒；后续会优先使用缓存。'
	};
}

function autoInit(opts) {
	if (!opts.source) return;
	autoWarmupPromise = (async () => {
		try {
			console.error('[wow-mcp] Auto warmup from CLI args...');
			await runWarmup(opts);
		} catch (e) {
			autoWarmupError = e;
			console.error('[wow-mcp] Auto warmup failed:', e.message);
		}
	})();
}

async function queryDb2({ mode = 'rows', table, ids, fields, filter, search, limit = 50 }) {
	await ensureReady();
	if (!table)
		throw new Error('table is required');

	const reader = db2[table];
	if (mode === 'schema') {
		const allRows = await reader.getAllRows();
		const schema = {};
		for (const [name, type] of reader.schema)
			schema[name] = Array.isArray(type) ? `${type[0].description}[${type[1]}]` : type.description;
		return { table, rowCount: allRows.size, fields: schema };
	}

	let rows = [];
	if (mode === 'search') {
		if (!search?.field || search.query === undefined)
			throw new Error('mode=search requires search.field and search.query');
		const lowerQuery = String(search.query).toLowerCase();
		for (const [, row] of await reader.getAllRows()) {
			const val = row[search.field];
			if (val !== undefined && String(val).toLowerCase().includes(lowerQuery)) {
				rows.push(row);
				if (rows.length >= limit) break;
			}
		}
	} else {
		let fKey, fVal, numVal;
		if (filter) {
			[fKey, fVal] = filter.split('=');
			fKey = fKey?.trim();
			fVal = (fVal || '').trim();
			numVal = Number(fVal);
		}
		const matchesFilter = (row) => !fKey || (row[fKey] !== undefined && (String(row[fKey]) === fVal || row[fKey] === numVal));
		if (ids?.length) {
			for (const id of ids) {
				const row = await reader.getRow(id);
				if (row && matchesFilter(row))
					rows.push(row);
			}
		} else {
			for (const [, row] of await reader.getAllRows()) {
				if (matchesFilter(row)) {
					rows.push(row);
					if (rows.length >= limit) break;
				}
			}
		}
	}

	if (fields?.length) {
		rows = rows.map(row => {
			const out = {};
			for (const field of fields)
				if (row[field] !== undefined) out[field] = row[field];
			return out;
		});
	}

	return { table, mode, count: rows.length, rows };
}

async function batchSpellInfo(spellIds, maxDepth = 5) {
	const allEffects = {};
	for (const [, row] of await db2.SpellEffect.getAllRows()) {
		if (!allEffects[row.SpellID]) allEffects[row.SpellID] = [];
		allEffects[row.SpellID].push(row);
	}

	const allIds = new Set(spellIds);
	const triggers = {};
	const descRefs = {};
	const spellDescCache = {};
	let frontier = [...spellIds];
	let depth = 0;

	const addRef = (map, parent, child) => {
		if (!map[parent]) map[parent] = [];
		if (!map[parent].includes(child)) map[parent].push(child);
	};

	while (frontier.length > 0 && depth < maxDepth) {
		const nextFrontier = [];
		for (const sid of frontier) {
			for (const e of allEffects[sid] || []) {
				for (const child of [e.EffectTriggerSpell, e.Effect === 64 ? e.EffectMiscValue : null]) {
					if (child && child > 0 && child !== sid) {
						addRef(triggers, sid, child);
						if (!allIds.has(child)) {
							allIds.add(child);
							nextFrontier.push(child);
						}
					}
				}
			}

			const spellRow = await db2.Spell.getRow(sid);
			if (spellRow) {
				spellDescCache[sid] = { desc: spellRow.Description_lang, auraDesc: spellRow.AuraDescription_lang };
				const text = (spellRow.Description_lang || '') + ' ' + (spellRow.AuraDescription_lang || '');
				const re = /\$@spellname(\d+)|\$(\d{5,})[a-zA-Z]/g;
				let m;
				while ((m = re.exec(text)) !== null) {
					const refId = Number(m[1] || m[2]);
					if (refId && refId !== sid) {
						addRef(descRefs, sid, refId);
						if (!allIds.has(refId)) {
							allIds.add(refId);
							nextFrontier.push(refId);
						}
					}
				}
			}
		}
		frontier = nextFrontier;
		depth++;
	}

	const miscMap = {};
	for (const [, row] of await db2.SpellMisc.getAllRows())
		if (allIds.has(row.SpellID)) miscMap[row.SpellID] = row;

	const castTimeMap = {};
	const durationMap = {};
	const rangeMap = {};
	for (const misc of Object.values(miscMap)) {
		if (misc.CastingTimeIndex && !castTimeMap[misc.CastingTimeIndex]) {
			const r = await db2.SpellCastTimes.getRow(misc.CastingTimeIndex);
			if (r) castTimeMap[misc.CastingTimeIndex] = { Base: r.Base, Minimum: r.Minimum };
		}
		if (misc.DurationIndex && !durationMap[misc.DurationIndex]) {
			const r = await db2.SpellDuration.getRow(misc.DurationIndex);
			if (r) durationMap[misc.DurationIndex] = { Duration: r.Duration, MaxDuration: r.MaxDuration };
		}
		if (misc.RangeIndex && !rangeMap[misc.RangeIndex]) {
			const r = await db2.SpellRange.getRow(misc.RangeIndex);
			if (r) rangeMap[misc.RangeIndex] = { DisplayName: r.DisplayName_lang, RangeMin: r.RangeMin, RangeMax: r.RangeMax };
		}
	}

	const spells = {};
	for (const sid of allIds) {
		const nameRow = await db2.SpellName.getRow(sid);
		if (!spellDescCache[sid]) {
			const spellRow = await db2.Spell.getRow(sid);
			if (spellRow) spellDescCache[sid] = { desc: spellRow.Description_lang, auraDesc: spellRow.AuraDescription_lang };
		}
		const misc = miscMap[sid] || null;
		spells[sid] = {
			spellId: sid,
			name: nameRow ? nameRow.Name_lang : null,
			description: spellDescCache[sid]?.desc || null,
			auraDescription: spellDescCache[sid]?.auraDesc || null,
			isSeed: spellIds.includes(sid),
			triggeredBy: [],
			misc: misc ? {
				Attributes: misc.Attributes,
				SchoolMask: misc.SchoolMask,
				Speed: misc.Speed,
				SpellIconFileDataID: misc.SpellIconFileDataID,
				castTime: castTimeMap[misc.CastingTimeIndex] || null,
				duration: durationMap[misc.DurationIndex] || null,
				range: rangeMap[misc.RangeIndex] || null
			} : null,
			effects: (allEffects[sid] || []).map(e => ({
				EffectIndex: e.EffectIndex,
				Effect: e.Effect,
				EffectAura: e.EffectAura,
				EffectTriggerSpell: e.EffectTriggerSpell || null,
				EffectAuraPeriod: e.EffectAuraPeriod || null,
				EffectBasePoints: e.EffectBasePointsF,
				EffectMechanic: e.EffectMechanic || 0,
				ImplicitTarget: e.ImplicitTarget,
				EffectRadiusIndex: e.EffectRadiusIndex,
				EffectMiscValue: e.EffectMiscValue
			}))
		};
	}

	const addTriggeredBy = (refs) => {
		for (const [parent, children] of Object.entries(refs))
			for (const child of children)
				if (spells[child] && !spells[child].triggeredBy.includes(Number(parent)))
					spells[child].triggeredBy.push(Number(parent));
	};
	addTriggeredBy(triggers);
	addTriggeredBy(descRefs);

	return { seedCount: spellIds.length, totalCount: allIds.size, chainDepth: depth, triggers, descRefs, spells };
}

async function handleSpell({ action = 'info', spellIds = [], npcIds = [], maxDepth = 5 }) {
	await ensureReady();
	if (!spellIds.length)
		throw new Error('spellIds is required');

	if (action === 'auras') {
		const idSet = new Set(spellIds);
		const hasAura = new Set();
		for (const [, row] of await db2.SpellEffect.getAllRows())
			if (row.Effect === 6 && idSet.has(row.SpellID)) hasAura.add(row.SpellID);
		return { hasAura: [...hasAura], noAura: spellIds.filter(id => !hasAura.has(id)) };
	}

	if (action === 'summons') {
		const spellSet = new Set(spellIds);
		const npcSet = new Set(npcIds);
		const result = {};
		for (const [, row] of await db2.SpellEffect.getAllRows()) {
			if (row.Effect === 28 && spellSet.has(row.SpellID) && npcSet.has(row.EffectMiscValue)) {
				if (!result[row.SpellID]) result[row.SpellID] = [];
				if (!result[row.SpellID].includes(row.EffectMiscValue)) result[row.SpellID].push(row.EffectMiscValue);
			}
		}
		return result;
	}

	return batchSpellInfo(spellIds, maxDepth);
}

async function handleEncounter({ journalEncounterID }) {
	await ensureReady();
	if (!journalEncounterID)
		throw new Error('journalEncounterID is required');

	const sections = [];
	for (const [, row] of await db2.JournalEncounterSection.getAllRows())
		if (row.JournalEncounterID === journalEncounterID) sections.push(row);

	const sectionIds = new Set(sections.map(s => s.ID));
	const processed = sections.map(s => ({
		id: s.ID,
		title: s.Title_lang,
		bodyText: s.BodyText_lang,
		spellID: s.SpellID,
		iconFlags: s.IconFlags,
		type: s.Type,
		difficultyMask: s.DifficultyMask,
		iconCreatureDisplayInfoID: s.IconCreatureDisplayInfoID,
		orderIndex: s.OrderIndex,
		parentSectionID: s.ParentSectionID,
		firstChildSectionID: s.FirstChildSectionID,
		nextSiblingSectionID: s.NextSiblingSectionID
	}));

	const byParent = {};
	for (const s of processed) {
		if (!byParent[s.parentSectionID]) byParent[s.parentSectionID] = [];
		byParent[s.parentSectionID].push(s);
	}
	const attachChildren = (node) => {
		const children = byParent[node.id] || [];
		if (children.length) {
			node.children = children.sort((a, b) => a.orderIndex - b.orderIndex);
			for (const child of node.children) attachChildren(child);
		}
		return node;
	};
	const tree = processed
		.filter(s => s.parentSectionID === 0 || !sectionIds.has(s.parentSectionID))
		.sort((a, b) => a.orderIndex - b.orderIndex)
		.map(attachChildren);
	const spellIds = sections.filter(s => s.SpellID > 0).map(s => s.SpellID);

	return { journalEncounterID, sectionCount: sections.length, spellCount: spellIds.length, spellIds, sections: tree };
}

async function handleFile({ action = 'lookup', fileDataID, search, extension, limit = 50 }) {
	await ensureReady();
	if (action === 'extractIcon') {
		if (fileDataID === undefined)
			throw new Error('fileDataID is required');
		const savePath = path.join(OUTPUT_DIR, `${fileDataID}.png`);
		if (fs.existsSync(savePath))
			return { status: 'cached', path: savePath };
		const data = await cascSource.getFile(fileDataID);
		const blp = new BLPFile(data);
		await blp.saveToPNG(savePath);
		return { status: 'extracted', path: savePath };
	}

	await ensureListfileLoaded();
	if (action === 'lookup') {
		if (fileDataID === undefined)
			throw new Error('fileDataID is required');
		return { fileDataID, fileName: listfile.getByID(fileDataID) || 'unknown' };
	}
	if (action === 'search') {
		if (!search)
			throw new Error('action=search requires search');
		const results = listfile.getFilteredEntries(search);
		return { search, total: results.length, returned: Math.min(results.length, limit), files: results.slice(0, limit) };
	}
	if (action === 'extension') {
		if (!extension)
			throw new Error('action=extension requires extension');
		const entries = listfile.getFilenamesByExtension(extension);
		return { extension, total: entries.length, returned: Math.min(entries.length, limit), files: entries.slice(0, limit) };
	}
	throw new Error('Unknown file action: ' + action);
}

async function main() {
	const startupOptions = parseArgs();

	const { McpServer } = await import('@modelcontextprotocol/sdk/server/mcp.js');
	const { StdioServerTransport } = await import('@modelcontextprotocol/sdk/server/stdio.js');
	const { z } = await import('zod');

	const server = new McpServer({
		name: 'wow-casc',
		version: '1.0.0',
		description: 'World of Warcraft CASC data query server. Call wow_warmup first; it may take up to 240 seconds.'
	});

	server.registerTool('wow_warmup', {
		title: '初始化并预热 WoW 数据',
		description: '首选初始化入口。无参数调用会列出本地/CDN、区域、Retail/Classic/PTR/Beta 等选择；带参数后连接、加载 build、校验缓存并预热。',
		inputSchema: z.object({
			source: z.enum(['local', 'remote']).optional(),
			path: z.string().optional(),
			region: z.enum(['cn', 'us', 'eu', 'kr', 'tw']).optional(),
			product: z.enum(['wow', 'wow_classic', 'wow_classic_titan', 'wow_classic_era', 'wowt', 'wowxptr']).optional(),
			buildIndex: z.number().optional(),
			forceReload: z.boolean().optional(),
			warm: z.object({
				listfile: z.boolean().optional(),
				dbdManifest: z.boolean().optional(),
				tables: z.array(z.string()).optional()
			}).optional()
		})
	}, async (args) => {
		try { return textResult(await runWarmup(args || {})); }
		catch (e) { return errorResult(e); }
	});

	server.registerTool('wow_db2', {
		title: '查询 DB2',
		description: '统一 DB2 工具：schema/rows/search。调用前先执行 wow_warmup。',
		inputSchema: z.object({
			mode: z.enum(['schema', 'rows', 'search']).default('rows'),
			table: z.string(),
			ids: z.array(z.number()).optional(),
			fields: z.array(z.string()).optional(),
			filter: z.string().optional(),
			search: z.object({ field: z.string(), query: z.string() }).optional(),
			limit: z.number().default(50)
		})
	}, async (args) => {
		try { return textResult(await queryDb2(args)); }
		catch (e) { return errorResult(e); }
	});

	server.registerTool('wow_spell', {
		title: '查询技能',
		description: '统一技能工具：info=递归技能链，auras=检测光环，summons=检测召唤 NPC。',
		inputSchema: z.object({
			action: z.enum(['info', 'auras', 'summons']).default('info'),
			spellIds: z.array(z.number()),
			npcIds: z.array(z.number()).optional(),
			maxDepth: z.number().default(5)
		})
	}, async (args) => {
		try { return textResult(await handleSpell(args)); }
		catch (e) { return errorResult(e); }
	});

	server.registerTool('wow_encounter', {
		title: '查询 Encounter',
		description: '查询 JournalEncounter 关联技能和章节树。',
		inputSchema: z.object({ journalEncounterID: z.number() })
	}, async (args) => {
		try { return textResult(await handleEncounter(args)); }
		catch (e) { return errorResult(e); }
	});

	server.registerTool('wow_file', {
		title: '查询文件和图标',
		description: '统一文件工具：lookup/search/extension/extractIcon。',
		inputSchema: z.object({
			action: z.enum(['lookup', 'search', 'extension', 'extractIcon']).default('lookup'),
			fileDataID: z.number().optional(),
			search: z.string().optional(),
			extension: z.string().optional(),
			limit: z.number().default(50)
		})
	}, async (args) => {
		try { return textResult(await handleFile(args)); }
		catch (e) { return errorResult(e); }
	});

	const transport = new StdioServerTransport();
	await server.connect(transport);
	autoInit(startupOptions);
}

main().catch(e => { console.error(e); process.exit(1); });
