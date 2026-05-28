/*!
	wow.export (https://github.com/Kruithne/wow.export)
	Authors: Kruithne <kruithne@gmail.com>, Marlamin <marlamin@marlamin.com>
	License: MIT
 */
const WDCReader = require('../db/WDCReader');

const cache = new Map();

const preload_proxy = new Proxy({}, {
	get(target, table_name) {
		if (typeof table_name !== 'string')
			return undefined;

		return async () => {
			const file_path = `DBFilesClient/${table_name}.db2`;

			if (cache.has(table_name)) {
				const existing = cache.get(table_name);
				if (!existing.isLoaded)
					await existing.parse();
				existing.preload();
				return existing;
			}

			const reader = new WDCReader(file_path);
			await reader.parse();
			reader.preload();

			const wrapper = create_wrapper(reader);
			cache.set(table_name, wrapper);
			return wrapper;
		};
	}
});

const create_wrapper = (reader) => {
	let parse_promise = null;

	return new Proxy(reader, {
		get(reader_target, prop) {
			const value = reader_target[prop];

			if (typeof value === 'function') {
				// getRelationRows requires preload() to have been called
				if (prop === 'getRelationRows') {
					return function(...args) {
						if (!reader_target.isLoaded)
							throw new Error('Table must be loaded before calling getRelationRows. Use db2.preload.' + reader_target.fileName.split('/').pop().replace('.db2', '') + '() first.');

						if (reader_target.rows === null)
							throw new Error('Table must be preloaded before calling getRelationRows. Use db2.preload.' + reader_target.fileName.split('/').pop().replace('.db2', '') + '() first.');

						return value.apply(reader_target, args);
					};
				}

				return async function(...args) {
					if (!reader_target.isLoaded) {
						if (parse_promise === null)
							parse_promise = reader_target.parse();

						await parse_promise;
					}

					return value.apply(reader_target, args);
				};
			}

			return value;
		}
	});
};

const db2_proxy = new Proxy({ preload: preload_proxy }, {
	get(target, table_name) {
		if (table_name === 'preload')
			return preload_proxy;

		// Return helper functions directly
		if (table_name === 'getRowsByForeignKey')
			return getRowsByForeignKey;
		if (table_name === 'streamAllRows')
			return streamAllRows;

		if (typeof table_name !== 'string')
			return undefined;

		if (cache.has(table_name))
			return cache.get(table_name);

		const file_path = `DBFilesClient/${table_name}.db2`;
		const reader = new WDCReader(file_path);
		const wrapper = create_wrapper(reader);

		cache.set(table_name, wrapper);
		return wrapper;
	}
});

/**
 * Get rows by foreign key value using relationship lookup.
 * Fast path: uses pre-built relationship index from WDCReader.
 * Slow path: streams and filters when no relationship data.
 * @param {string} tableName
 * @param {string} fkField
 * @param {number} fkValue
 * @returns {Promise<Array>}
 */
async function getRowsByForeignKey(tableName, fkField, fkValue) {
        const table = db2_proxy[tableName];
        await table.parse();

        // Fast path: use pre-built relationship index
        // relationshipLookup maps foreignKeyValue -> [recordIDs]
        if (table.relationshipLookup && table.relationshipLookup.has(fkValue)) {
                const recordIDs = table.relationshipLookup.get(fkValue);
                const results = [];
                for (const id of recordIDs) {
                        const row = await table.getRow(id);
                        if (row) results.push(row);
                }
                return results;
        }

        // Slow path: stream and filter (for tables without relationship data)
        const results = [];
        for (const [, row] of await table.getAllRows()) {
                if (row[fkField] === fkValue) results.push(row);
        }
        return results;
}

/**
 * Stream all rows one-by-one to callback without building full array.
 * Uses WDCReader's sequential section iteration for O(1) memory.
 * Callback receives each row, can return false to stop early.
 * @param {string} tableName
 * @param {(row: object) => boolean|void} callback
 * @returns {Promise<void>}
 */
async function streamAllRows(tableName, callback) {
        const table = db2_proxy[tableName];
        await table.parse();

        if (!table.isLoaded) throw new Error('Table not loaded');

	// Access internal sections for streaming
	for (let sectionIndex = 0; sectionIndex < table.sections.length; sectionIndex++) {
		const section = table.sections[sectionIndex];
		if (section.isEncrypted) continue;

		const hasIDMap = section.idList.length > 0;
		const emptyIDMap = hasIDMap && section.idList.every(id => id === 0);

                for (let i = 0; i < section.header.recordCount; i++) {
                        let recordID;
                        if (hasIDMap && emptyIDMap) {
                                recordID = i;
                        } else if (hasIDMap) {
                                recordID = section.idList[i];
                        }

                        const row = await table._readRecordFromSection(sectionIndex, i, recordID);
                        if (row !== null) {
                                const finalRow = recordID !== undefined
                                        ? { ...row, ID: recordID }
                                        : row;

				// Callback can return false to stop early
				if (callback(finalRow) === false) return;
			}
		}
	}

        // Inflate copy table rows at end
        for (const [destID, srcID] of table.copyTable) {
                const src = await table.getRow(srcID);
                if (src !== undefined && src !== null) {
                        const rowData = { ...src, ID: destID };
                        if (callback(rowData) === false) return;
                }
        }
}

module.exports = db2_proxy;
