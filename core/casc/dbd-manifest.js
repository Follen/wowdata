/*!
	wow.export (https://github.com/Kruithne/wow.export)
	Authors: Kruithne <kruithne@gmail.com>
	License: MIT
 */
const core = require('../core');
const log = require('../log');
const generics = require('../generics');
const constants = require('../constants');
const fsp = require('fs').promises;
const path = require('path');

let is_preloaded = false;
let preload_promise = null;
let table_to_id = new Map();
let id_to_table = new Map();

const manifest_cache_file = path.join(constants.CACHE.DIR_DBD, 'dbd-manifest.json');

function ingestManifest(manifest_data) {
	if (!Array.isArray(manifest_data))
		throw new Error('DBD manifest cache is not an array');

	table_to_id = new Map();
	id_to_table = new Map();
	for (const entry of manifest_data) {
		if (entry.tableName && entry.db2FileDataID) {
			table_to_id.set(entry.tableName, entry.db2FileDataID);
			id_to_table.set(entry.db2FileDataID, entry.tableName);
		}
	}

	if (table_to_id.size === 0)
		throw new Error('DBD manifest contains no table mappings');
}

/**
 * preload the dbd manifest from configured urls
 */
function preload() {
	if (preload_promise !== null)
		return;

	preload_promise = (async () => {
		try {
			try {
				const cached = await generics.readJSON(manifest_cache_file, false);
				ingestManifest(cached);
				log.write('loaded cached dbd manifest with %d entries', table_to_id.size);
				is_preloaded = true;
				return;
			} catch (e) {
				log.write('cached dbd manifest unavailable or invalid: %s', e.message);
			}

			const dbd_filename_url = core.view.config.dbdFilenameURL;
			const dbd_filename_fallback_url = core.view.config.dbdFilenameFallbackURL;
			const raw = await generics.downloadFile([dbd_filename_url, dbd_filename_fallback_url]);
			const manifest_data = raw.readJSON();
			ingestManifest(manifest_data);
			await fsp.mkdir(path.dirname(manifest_cache_file), { recursive: true });
			await fsp.writeFile(manifest_cache_file, raw.raw);

			log.write('preloaded dbd manifest with %d entries', table_to_id.size);
			is_preloaded = true;
		} catch (e) {
			log.write('failed to preload dbd manifest: %s', e.message);
			is_preloaded = true;
		}
	})();
}

/**
 * prepare the manifest for use, awaiting preload if necessary
 * @returns {Promise<boolean>}
 */
async function prepareManifest() {
	if (is_preloaded)
		return true;

	if (preload_promise === null)
		preload();

	await preload_promise;
	return true;
}

/**
 * get table name by filedataid
 * @param {number} id
 * @returns {string|undefined}
 */
function getByID(id) {
	return id_to_table.get(id);
}

/**
 * get filedataid by table name
 * @param {string} table_name
 * @returns {number|undefined}
 */
function getByTableName(table_name) {
	return table_to_id.get(table_name);
}

/**
 * get all table names
 * @returns {string[]}
 */
function getAllTableNames() {
	return Array.from(table_to_id.keys()).sort();
}

module.exports = { preload, prepareManifest, getByID, getByTableName, getAllTableNames };
