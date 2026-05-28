// MCP server unit test - tests the full flow without MCP protocol
BUILD_RELEASE = true;
globalThis.requestAnimationFrame = (cb) => setImmediate(cb);

const path = require('path');
const fs = require('fs');
require('./core/shim');

const core = require('./core/core');
const constants = require('./core/constants');
const generics = require('./core/generics');
const LocaleFlag = require('./core/casc/locale-flags').flags;
const CASCRemote = require('./core/casc/casc-source-remote');
const BLPFile = require('./core/casc/blp');
const db2 = require('./core/casc/db2');
const listfile = require('./core/casc/listfile');

// Mock core.view (same as index.js)
core.view = core.makeNewView();
core.view.$watch = (prop, cb, opts) => {
	if (opts && opts.immediate) {
		if (prop === 'config.cascLocale') cb(LocaleFlag.zhCN);
		else cb(undefined);
	}
	return () => {};
};
try { core.view.config = require('./core/config').getDefaults(); } catch(e) { core.view.config = {}; }

let cascSource = null;

async function test_connect() {
	console.log('\n=== TEST: wow_connect (remote, cn) ===');
	cascSource = new CASCRemote('cn');
	await cascSource.init();
	const products = cascSource.getProductList();
	console.log('OK - products:', products.length);
	console.log('First:', products[0]?.label);
	return products;
}

async function test_load(buildIndex) {
	console.log('\n=== TEST: wow_load_build (' + buildIndex + ') ===');
	await cascSource.load(buildIndex);
	console.log('OK - build loaded');
}

async function test_db2_schema(table) {
	console.log('\n=== TEST: wow_db2_schema (' + table + ') ===');
	const reader = db2[table];
	console.log('reader type:', typeof reader);
	console.log('reader.isLoaded:', reader.isLoaded);

	const allRows = await reader.getAllRows();
	console.log('allRows type:', typeof allRows, 'isMap:', allRows instanceof Map);
	console.log('allRows.size:', allRows.size);
	console.log('reader.isLoaded after getAllRows:', reader.isLoaded);

	const schema = {};
	console.log('reader.schema type:', typeof reader.schema, 'isMap:', reader.schema instanceof Map);
	for (const [name, type] of reader.schema)
		schema[name] = type.toString();
	console.log('OK - fields:', Object.keys(schema).length, 'rows:', allRows.size);
	console.log('Fields:', Object.keys(schema).join(', '));
}

async function test_search(table, field, query) {
	console.log('\n=== TEST: wow_search_db2 (' + table + '.' + field + ' ~ "' + query + '") ===');
	const reader = db2[table];
	const allRows = await reader.getAllRows();
	const results = [];
	const lq = query.toLowerCase();
	for (const [, row] of allRows) {
		const val = row[field];
		if (val !== undefined && String(val).toLowerCase().includes(lq)) {
			results.push(row);
			if (results.length >= 5) break;
		}
	}
	console.log('OK - found:', results.length);
	if (results.length > 0) console.log('First:', JSON.stringify(results[0]).substring(0, 200));
}

async function test_listfile() {
	console.log('\n=== TEST: wow_list_files (search "spellname") ===');
	const results = listfile.getFilteredEntries('spellname');
	console.log('OK - found:', results.length);
	if (results.length > 0) console.log('First:', JSON.stringify(results[0]));
}

async function main() {
	try {
		await test_connect();
		await test_load(0);
		await test_db2_schema('SpellName');
		await test_search('SpellName', 'Name_lang', '火球');
		await test_listfile();
		console.log('\n=== ALL TESTS PASSED ===');
	} catch (e) {
		console.error('\n=== TEST FAILED ===');
		console.error(e);
	}
	process.exit(0);
}

main();
