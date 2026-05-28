// MCP uses stdout for JSON-RPC, so suppress all console.log from core modules
BUILD_RELEASE = true;
console.log = console.error;
globalThis.requestAnimationFrame = (cb) => setImmediate(cb);

const path = require('path');
const fs = require('fs');

// Initialize wow.export compatibility shim
require('./core/shim');

// Load core modules
const core = require('./core/core');
const constants = require('./core/constants');
const generics = require('./core/generics');
const LocaleFlag = require('./core/casc/locale-flags').flags;
const CASCRemote = require('./core/casc/casc-source-remote');
const CASCLocal = require('./core/casc/casc-source-local');
const BLPFile = require('./core/casc/blp');
const db2 = require('./core/casc/db2');
const listfile = require('./core/casc/listfile');

// State
let cascSource = null;
let isCascReady = false;
let currentRegion = 'cn';

const OUTPUT_DIR = path.join(__dirname, 'output');
if (!fs.existsSync(OUTPUT_DIR)) fs.mkdirSync(OUTPUT_DIR, { recursive: true });

// Region → locale mapping
const getLocaleForRegion = (region) => {
	switch (region) {
		case 'cn': return LocaleFlag.zhCN;
		case 'tw': return LocaleFlag.zhTW;
		case 'kr': return LocaleFlag.koKR;
		default: return LocaleFlag.enUS;
	}
};

// Initialize core.view mock (same pattern as SpellGen server.js)
if (!core.view) {
	core.view = core.makeNewView();
	core.view.$watch = (property, callback, options) => {
		if (options && options.immediate) {
			if (property === 'config.cascLocale')
				callback(getLocaleForRegion(currentRegion));
			else
				callback(undefined);
		}
		return () => {};
	};
	// Default config from wow.export's default_config.jsonc (essential URLs for CASC/DB2)
	core.view.config = {
		enableBinaryListfile: false,
		listfileBinarySource: 'https://www.kruithne.net/wow.export/data/listfile/%s',
		listfileURL: 'https://github.com/wowdev/wow-listfile/releases/latest/download/community-listfile.csv',
		listfileFallbackURL: 'https://www.kruithne.net/wow.export/data/listfile/master',
		listfileCacheRefresh: 3,
		dbdURL: 'https://raw.githubusercontent.com/wowdev/WoWDBDefs/refs/heads/master/definitions/%s.dbd',
		dbdFallbackURL: 'https://www.kruithne.net/wow.export/data/dbd/?def=%s',
		dbdFilenameURL: 'https://raw.githubusercontent.com/wowdev/WoWDBDefs/refs/heads/master/manifest.json',
		dbdFilenameFallbackURL: 'https://www.kruithne.net/wow.export/data/dbd',
		tactKeysURL: 'https://raw.githubusercontent.com/wowdev/TACTKeys/master/WoW.txt',
		tactKeysFallbackURL: 'https://www.kruithne.net/wow.export/data/tact/wow',
		cascLocale: 2,
		enableUnknownFiles: true,
		cacheExpiry: 7,
		exportDirectory: '',
		pathFormat: 'win32'
	};
}

// Patch progressLoadingScreen to skip generics.redraw() (no UI in MCP)
core.progressLoadingScreen = async (text) => {
	core.view.loadingProgress = text;
	console.error('[wow-mcp] ' + text);
};

// Parse CLI args for auto-connect/load
function parseArgs() {
	const args = process.argv.slice(2);
	const opts = {};
	for (let i = 0; i < args.length; i++) {
		if (args[i] === '--local' && args[i + 1]) opts.source = 'local', opts.path = args[++i];
		else if (args[i] === '--remote') opts.source = 'remote', opts.region = args[++i] || 'cn';
		else if (args[i] === '--build' && args[i + 1]) opts.build = parseInt(args[++i], 10);
		else if (args[i] === '--region' && args[i + 1]) opts.region = args[++i];
	}
	return opts;
}

// Auto-connect and load CASC before MCP starts (avoids tool call timeout)
async function autoInit(opts) {
	if (!opts.source) return;
	currentRegion = opts.region || 'cn';
	console.error('[wow-mcp] Auto-connecting: ' + opts.source + (opts.path ? ' ' + opts.path : '') + ' region=' + currentRegion);

	if (opts.source === 'local') {
		core.view.selectedCDNRegion = { tag: currentRegion };
		cascSource = new CASCLocal(opts.path);
	} else {
		cascSource = new CASCRemote(currentRegion);
	}
	await cascSource.init();
	console.error('[wow-mcp] Connected. Products: ' + cascSource.getProductList().length);

	if (opts.build !== undefined) {
		console.error('[wow-mcp] Loading build index ' + opts.build + '...');
		await cascSource.load(opts.build);
		isCascReady = true;
		console.error('[wow-mcp] CASC ready.');
	}
}

// MCP Server main
async function main() {
	const cliOpts = parseArgs();
	await autoInit(cliOpts);

	const { McpServer } = await import('@modelcontextprotocol/sdk/server/mcp.js');
	const { StdioServerTransport } = await import('@modelcontextprotocol/sdk/server/stdio.js');
	const { z } = await import('zod');

	const server = new McpServer({
		name: 'wow-casc',
		version: '1.0.0',
		description: 'World of Warcraft CASC data query server'
	});

	// Tool 1: wow_connect
	server.registerTool('wow_connect', {
		title: '连接 CASC 存储',
		description: '连接到魔兽世界 CASC 数据源（本地游戏目录或远程 CDN），返回可用构建版本列表',
		inputSchema: z.object({
			source: z.enum(['local', 'remote']).describe('数据源类型'),
			path: z.string().optional().describe('本地游戏目录路径（source=local 时必填）'),
			region: z.enum(['cn', 'us', 'eu', 'kr', 'tw']).default('cn').describe('CDN 区域')
		})
	}, async ({ source, path: wowPath, region }) => {
		try {
			currentRegion = region || 'cn';
			if (source === 'local') {
				if (!wowPath) return { content: [{ type: 'text', text: 'Error: path is required for local source' }], isError: true };
				core.view.selectedCDNRegion = { tag: currentRegion };
				cascSource = new CASCLocal(wowPath);
			} else {
				cascSource = new CASCRemote(region || 'cn');
			}
			await cascSource.init();
			const products = cascSource.getProductList();
			return { content: [{ type: 'text', text: JSON.stringify({ success: true, products }, null, 2) }] };
		} catch (e) {
			return { content: [{ type: 'text', text: 'Error: ' + e.message }], isError: true };
		}
	});

	// Tool 2: wow_load_build
	server.registerTool('wow_load_build', {
		title: '加载构建版本',
		description: '加载指定的构建版本索引（先调用 wow_connect 获取版本列表）',
		inputSchema: z.object({
			buildIndex: z.number().describe('构建版本索引（从 wow_connect 返回的列表中选择）')
		})
	}, async ({ buildIndex }) => {
		try {
			if (!cascSource) return { content: [{ type: 'text', text: 'Error: 请先调用 wow_connect' }], isError: true };
			await cascSource.load(buildIndex);
			isCascReady = true;
			return { content: [{ type: 'text', text: JSON.stringify({ success: true, message: '构建版本加载完成' }) }] };
		} catch (e) {
			return { content: [{ type: 'text', text: 'Error: ' + e.message }], isError: true };
		}
	});

	// Tool 3: wow_db2_schema
	server.registerTool('wow_db2_schema', {
		title: '获取 DB2 表结构',
		description: '获取指定 DB2 表的字段名、类型和总行数',
		inputSchema: z.object({
			table: z.string().describe('DB2 表名，如 SpellName, Item, Creature')
		})
	}, async ({ table }) => {
		try {
			if (!isCascReady) return { content: [{ type: 'text', text: 'Error: CASC 未就绪，请先 connect + load' }], isError: true };
			const reader = db2[table];
			const allRows = await reader.getAllRows();
			const schema = {};
			for (const [name, type] of reader.schema)
				schema[name] = Array.isArray(type) ? `${type[0].description}[${type[1]}]` : type.description;
			return { content: [{ type: 'text', text: JSON.stringify({ table, rowCount: allRows.size, fields: schema }, null, 2) }] };
		} catch (e) {
			return { content: [{ type: 'text', text: 'Error: ' + e.message }], isError: true };
		}
	});

	// Tool 4: wow_query_db2
	server.registerTool('wow_query_db2', {
		title: '查询 DB2 表数据',
		description: '查询任意 DB2 表，支持按 ID 精确查询、字段过滤、条件筛选。示例表：SpellName, SpellEffect, SpellMisc, Item, ItemSparse, Creature, Map, Achievement 等',
		inputSchema: z.object({
			table: z.string().describe('DB2 表名'),
			ids: z.array(z.number()).optional().describe('精确查询的行 ID 列表'),
			fields: z.array(z.string()).optional().describe('只返回指定字段'),
			filter: z.string().optional().describe('简单过滤: field=value'),
			limit: z.number().default(50).describe('最大返回行数')
		})
	}, async ({ table, ids, fields, filter, limit }) => {
		try {
			if (!isCascReady) return { content: [{ type: 'text', text: 'Error: CASC 未就绪' }], isError: true };
			const reader = db2[table];
			let rows = [];
			const maxRows = limit || 50;

			// Parse filter upfront
			let fKey, fVal, numVal;
			if (filter) {
				[fKey, fVal] = filter.split('=');
				if (fKey) { fKey = fKey.trim(); fVal = (fVal || '').trim(); numVal = Number(fVal); }
			}

			const matchesFilter = (r) => {
				if (!fKey) return true;
				const v = r[fKey];
				return v !== undefined && (String(v) === fVal || v === numVal);
			};

			if (ids && ids.length > 0) {
				for (const id of ids) {
					const row = await reader.getRow(id);
					if (row && matchesFilter(row)) rows.push(row);
				}
			} else {
				// Filter DURING iteration, then apply limit
				for (const [, row] of await reader.getAllRows()) {
					if (matchesFilter(row)) {
						rows.push(row);
						if (rows.length >= maxRows) break;
					}
				}
			}

			// Project fields
			if (fields && fields.length > 0) {
				rows = rows.map(r => {
					const out = {};
					for (const f of fields) if (r[f] !== undefined) out[f] = r[f];
					return out;
				});
			}

			return { content: [{ type: 'text', text: JSON.stringify({ table, count: rows.length, rows }, null, 2) }] };
		} catch (e) {
			return { content: [{ type: 'text', text: 'Error: ' + e.message }], isError: true };
		}
	});

	// Tool 5: wow_search_db2
	server.registerTool('wow_search_db2', {
		title: '文本搜索 DB2 表',
		description: '在 DB2 表的指定字符串字段中做子串匹配搜索',
		inputSchema: z.object({
			table: z.string().describe('DB2 表名'),
			field: z.string().describe('要搜索的字段名'),
			query: z.string().describe('搜索关键词（子串匹配）'),
			limit: z.number().default(20).describe('最大返回行数')
		})
	}, async ({ table, field, query, limit }) => {
		try {
			if (!isCascReady) return { content: [{ type: 'text', text: 'Error: CASC 未就绪' }], isError: true };
			const reader = db2[table];
			const results = [];
			const lowerQuery = query.toLowerCase();
			for (const [, row] of await reader.getAllRows()) {
				const val = row[field];
				if (val !== undefined && String(val).toLowerCase().includes(lowerQuery)) {
					results.push(row);
					if (results.length >= (limit || 20)) break;
				}
			}
			return { content: [{ type: 'text', text: JSON.stringify({ table, field, query, count: results.length, rows: results }, null, 2) }] };
		} catch (e) {
			return { content: [{ type: 'text', text: 'Error: ' + e.message }], isError: true };
		}
	});

	// Tool 6: wow_extract_icon
	server.registerTool('wow_extract_icon', {
		title: '提取 BLP 图标为 PNG',
		description: '从 CASC 中提取 BLP 格式图标并保存为 PNG 文件',
		inputSchema: z.object({
			fileDataID: z.number().describe('图标的 fileDataID')
		})
	}, async ({ fileDataID }) => {
		try {
			if (!isCascReady) return { content: [{ type: 'text', text: 'Error: CASC 未就绪' }], isError: true };
			const savePath = path.join(OUTPUT_DIR, `${fileDataID}.png`);
			if (fs.existsSync(savePath))
				return { content: [{ type: 'text', text: JSON.stringify({ status: 'cached', path: savePath }) }] };

			const data = await cascSource.getFile(fileDataID);
			const blp = new BLPFile(data);
			await blp.saveToPNG(savePath);
			return { content: [{ type: 'text', text: JSON.stringify({ status: 'extracted', path: savePath }) }] };
		} catch (e) {
			return { content: [{ type: 'text', text: 'Error: ' + e.message }], isError: true };
		}
	});

	// Tool 7: wow_list_files
	server.registerTool('wow_list_files', {
		title: '浏览/搜索 CASC 文件列表',
		description: '浏览或搜索 CASC 存储中的文件列表（模型、贴图、音效等）。三种模式：按 fileDataID 精确查找、按关键词搜索文件名、按扩展名过滤',
		inputSchema: z.object({
			fileDataID: z.number().optional().describe('精确查找单个文件的路径'),
			search: z.string().optional().describe('子串搜索文件名'),
			extension: z.string().optional().describe('按扩展名过滤，如 .blp, .m2, .db2'),
			limit: z.number().default(50).describe('最大返回条数')
		})
	}, async ({ fileDataID, search, extension, limit }) => {
		try {
			if (!isCascReady) return { content: [{ type: 'text', text: 'Error: CASC 未就绪' }], isError: true };

			// Mode 1: lookup by ID
			if (fileDataID !== undefined) {
				const fileName = listfile.getByID(fileDataID);
				return { content: [{ type: 'text', text: JSON.stringify({ fileDataID, fileName: fileName || 'unknown' }) }] };
			}

			// Mode 2: search by substring
			if (search) {
				const results = listfile.getFilteredEntries(search);
				const limited = results.slice(0, limit || 50);
				return { content: [{ type: 'text', text: JSON.stringify({ search, total: results.length, returned: limited.length, files: limited }, null, 2) }] };
			}

			// Mode 3: filter by extension
			if (extension) {
				const entries = listfile.getFilenamesByExtension(extension);
				const limited = entries.slice(0, limit || 50);
				return { content: [{ type: 'text', text: JSON.stringify({ extension, total: entries.length, returned: limited.length, files: limited }, null, 2) }] };
			}

			return { content: [{ type: 'text', text: 'Error: 请提供 fileDataID、search 或 extension 参数' }], isError: true };
		} catch (e) {
			return { content: [{ type: 'text', text: 'Error: ' + e.message }], isError: true };
		}
	});

	// Tool 8: wow_check_spell_auras
	server.registerTool('wow_check_spell_auras', {
		title: '批量检测技能光环',
		description: '一次性扫描 SpellEffect 表，返回给定 spellID 列表中哪些有 Effect=6 (APPLY_AURA)',
		inputSchema: z.object({
			spellIds: z.array(z.number()).describe('要检测的 spellID 列表')
		})
	}, async ({ spellIds }) => {
		try {
			if (!isCascReady) return { content: [{ type: 'text', text: 'Error: CASC 未就绪' }], isError: true };
			const idSet = new Set(spellIds);
			const hasAura = new Set();
			for (const [, row] of await db2.SpellEffect.getAllRows()) {
				if (row.Effect === 6 && idSet.has(row.SpellID))
					hasAura.add(row.SpellID);
			}
			const result = { hasAura: [...hasAura], noAura: spellIds.filter(id => !hasAura.has(id)) };
			return { content: [{ type: 'text', text: JSON.stringify(result) }] };
		} catch (e) {
			return { content: [{ type: 'text', text: 'Error: ' + e.message }], isError: true };
		}
	});

	// Tool 9: wow_get_npc_summons
	server.registerTool('wow_get_npc_summons', {
		title: '查询技能召唤的NPC',
		description: '扫描 SpellEffect 表，找出给定技能列表中哪些技能会召唤给定npcID列表中的NPC（Effect=28 SUMMON，EffectMiscValue=npcID）。用于检测Boss阶段分身关系。',
		inputSchema: z.object({
			spellIds: z.array(z.number()).describe('要检测的技能ID列表'),
			npcIds: z.array(z.number()).describe('要检测的NPC ID列表')
		})
	}, async ({ spellIds, npcIds }) => {
		try {
			if (!isCascReady) return { content: [{ type: 'text', text: 'Error: CASC 未就绪' }], isError: true };
			const spellSet = new Set(spellIds);
			const npcSet = new Set(npcIds);
			const result = {}; // spellId -> [npcId]
			for (const [, row] of await db2.SpellEffect.getAllRows()) {
				if (row.Effect === 28 && spellSet.has(row.SpellID) && npcSet.has(row.EffectMiscValue)) {
					if (!result[row.SpellID]) result[row.SpellID] = [];
					if (!result[row.SpellID].includes(row.EffectMiscValue))
						result[row.SpellID].push(row.EffectMiscValue);
				}
			}
			return { content: [{ type: 'text', text: JSON.stringify(result) }] };
		} catch (e) {
			return { content: [{ type: 'text', text: 'Error: ' + e.message }], isError: true };
		}
	});

	// Tool: wow_batch_spell_info — 批量查询技能完整数据（含递归链追踪）
	server.registerTool('wow_batch_spell_info', {
		title: '批量查询技能完整信息（递归链）',
		description: '输入种子SpellID列表，自动递归追踪所有EffectTriggerSpell子技能，返回链上全部技能的完整数据（SpellName、SpellMisc、SpellEffect + 子表）。maxDepth控制递归深度（默认5）。',
		inputSchema: z.object({
			spellIds: z.array(z.number()).describe('种子 spellID 列表'),
			maxDepth: z.number().optional().describe('最大递归深度，默认5')
		})
	}, async ({ spellIds, maxDepth = 5 }) => {
		try {
			if (!isCascReady) return { content: [{ type: 'text', text: 'Error: CASC 未就绪' }], isError: true };

			// 1) 一次性扫描 SpellEffect 全表，建立 spellId -> [rows] 索引
			const allEffects = {};
			for (const [, row] of await db2.SpellEffect.getAllRows()) {
				if (!allEffects[row.SpellID]) allEffects[row.SpellID] = [];
				allEffects[row.SpellID].push(row);
			}

			// 2) 从种子ID出发，递归追踪 EffectTriggerSpell + 描述文本引用
			const allIds = new Set(spellIds);
			const triggers = {}; // parentId -> [childId]
			const descRefs = {}; // parentId -> [refId] (描述引用)
			const spellDescCache = {}; // sid -> { desc, auraDesc }
			let frontier = [...spellIds];
			let depth = 0;

			const addRef = (map, parent, child) => {
				if (!map[parent]) map[parent] = [];
				if (!map[parent].includes(child)) map[parent].push(child);
			};

			while (frontier.length > 0 && depth < maxDepth) {
				const nextFrontier = [];
				for (const sid of frontier) {
					// a) EffectTriggerSpell
					const effects = allEffects[sid] || [];
					for (const e of effects) {
						if (e.EffectTriggerSpell && e.EffectTriggerSpell > 0) {
							addRef(triggers, sid, e.EffectTriggerSpell);
							if (!allIds.has(e.EffectTriggerSpell)) {
								allIds.add(e.EffectTriggerSpell);
								nextFrontier.push(e.EffectTriggerSpell);
							}
						}
						// Follow Effect=64 (TRIGGER_SPELL) via EffectMiscValue
						if (e.Effect === 64 && e.EffectMiscValue && e.EffectMiscValue > 0 && e.EffectMiscValue !== e.EffectTriggerSpell) {
							addRef(triggers, sid, e.EffectMiscValue);
							if (!allIds.has(e.EffectMiscValue)) {
								allIds.add(e.EffectMiscValue);
								nextFrontier.push(e.EffectMiscValue);
							}
						}
					}
					// b) 描述文本中的技能引用 $@spellname123456 / $123456d / $123456s1 等
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

			// 3) 扫描 SpellMisc（只需匹配 allIds）
			const miscMap = {};
			for (const [, row] of await db2.SpellMisc.getAllRows()) {
				if (allIds.has(row.SpellID)) miscMap[row.SpellID] = row;
			}

			// 4) 收集子表索引
			const castTimeIds = new Set();
			const durationIds = new Set();
			const rangeIds = new Set();
			for (const misc of Object.values(miscMap)) {
				if (misc.CastingTimeIndex) castTimeIds.add(misc.CastingTimeIndex);
				if (misc.DurationIndex) durationIds.add(misc.DurationIndex);
				if (misc.RangeIndex) rangeIds.add(misc.RangeIndex);
			}

			// 5) 解析子表
			const castTimeMap = {};
			const durationMap = {};
			const rangeMap = {};
			for (const id of castTimeIds) {
				const r = await db2.SpellCastTimes.getRow(id);
				if (r) castTimeMap[id] = { Base: r.Base, Minimum: r.Minimum };
			}
			for (const id of durationIds) {
				const r = await db2.SpellDuration.getRow(id);
				if (r) durationMap[id] = { Duration: r.Duration, MaxDuration: r.MaxDuration };
			}
			for (const id of rangeIds) {
				const r = await db2.SpellRange.getRow(id);
				if (r) rangeMap[id] = { DisplayName: r.DisplayName_lang, RangeMin: r.RangeMin, RangeMax: r.RangeMax };
			}

			// 6) 组装每个技能的完整数据
			const result = {};
			for (const sid of allIds) {
				const nameRow = await db2.SpellName.getRow(sid);
				if (!spellDescCache[sid]) {
					const spellRow = await db2.Spell.getRow(sid);
					if (spellRow) spellDescCache[sid] = { desc: spellRow.Description_lang, auraDesc: spellRow.AuraDescription_lang };
				}
				const cached = spellDescCache[sid];
				const misc = miscMap[sid] || null;
				const effects = allEffects[sid] || [];

				const entry = {
					spellId: sid,
					name: nameRow ? nameRow.Name_lang : null,
					description: cached ? cached.desc : null,
					auraDescription: cached ? cached.auraDesc : null,
					isSeed: spellIds.includes(sid),
					triggeredBy: [],
					misc: null,
					effects: effects.map(e => ({
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

				if (misc) {
					entry.misc = {
						Attributes: misc.Attributes,
						SchoolMask: misc.SchoolMask,
						Speed: misc.Speed,
						SpellIconFileDataID: misc.SpellIconFileDataID,
						castTime: castTimeMap[misc.CastingTimeIndex] || null,
						duration: durationMap[misc.DurationIndex] || null,
						range: rangeMap[misc.RangeIndex] || null
					};
				}

				result[sid] = entry;
			}

			// 7) 填充 triggeredBy 反向引用（含描述引用）
			for (const [parent, children] of Object.entries(triggers)) {
				for (const child of children) {
					if (result[child]) result[child].triggeredBy.push(Number(parent));
				}
			}
			for (const [parent, refs] of Object.entries(descRefs)) {
				for (const ref of refs) {
					if (result[ref] && !result[ref].triggeredBy.includes(Number(parent))) {
						result[ref].triggeredBy.push(Number(parent));
					}
				}
			}

			return { content: [{ type: 'text', text: JSON.stringify({
				seedCount: spellIds.length,
				totalCount: allIds.size,
				chainDepth: depth,
				triggers,
				descRefs,
				spells: result
			}, null, 2) }] };
		} catch (e) {
			return { content: [{ type: 'text', text: 'Error: ' + e.message }], isError: true };
		}
	});

	// Tool: wow_encounter_spells — 查询副本Encounter关联技能
	server.registerTool('wow_encounter_spells', {
		title: '查询副本Encounter关联技能',
		description: '输入JournalEncounter ID，递归遍历JournalEncounterSection获取所有关联SpellID，返回分层section树和DifficultyMask',
		inputSchema: z.object({
			journalEncounterID: z.number().describe('JournalEncounter ID')
		})
	}, async ({ journalEncounterID }) => {
		try {
			if (!isCascReady) return { content: [{ type: 'text', text: 'Error: CASC 未就绪，请先 connect + load' }], isError: true };

			// Load all JournalEncounterSection rows
			const allSections = [];
			for (const [, row] of await db2.JournalEncounterSection.getAllRows()) {
				allSections.push(row);
			}

			// Filter sections for this encounter
			const sections = allSections.filter(s => s.JournalEncounterID === journalEncounterID);

			// Extract section data
			const sectionIds = new Set(sections.map(s => s.ID));
			const processedSections = sections.map(s => ({
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

			// Build tree: group by parentSectionID
			const byParent = {};
			for (const s of processedSections) {
				const pid = s.parentSectionID;
				if (!byParent[pid]) byParent[pid] = [];
				byParent[pid].push(s);
			}

			// Root sections: parentSectionID=0 or parent not in this encounter's sections
			const roots = processedSections.filter(s =>
				s.parentSectionID === 0 || !sectionIds.has(s.parentSectionID)
			);

			// Attach children recursively
			function attachChildren(node) {
				const children = byParent[node.id] || [];
				if (children.length > 0) {
					node.children = children.sort((a, b) => a.orderIndex - b.orderIndex);
					for (const child of node.children) attachChildren(child);
				}
				return node;
			}
			const tree = roots
				.sort((a, b) => a.orderIndex - b.orderIndex)
				.map(r => attachChildren(r));

			// Collect spell IDs
			const spellIds = sections.filter(s => s.SpellID > 0).map(s => s.SpellID);

			return { content: [{ type: 'text', text: JSON.stringify({
				journalEncounterID,
				sectionCount: sections.length,
				spellCount: spellIds.length,
				spellIds,
				sections: tree
			}, null, 2) }] };
		} catch (e) {
			return { content: [{ type: 'text', text: 'Error: ' + e.message }], isError: true };
		}
	});

	const transport = new StdioServerTransport();
	await server.connect(transport);
}

main().catch(e => { console.error(e); process.exit(1); });
