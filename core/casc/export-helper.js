const path = require('path');
const core = require('../core');

class ExportHelper {
	static replaceExtension(file, newExt = '') {
		const parsed = path.parse(file);
		return path.join(parsed.dir, parsed.name + newExt);
	}

	static getExportPath(file) {
		const exportDirectory = core.view.config.exportDirectory || '';
		const normalized = core.view.config.removePathSpaces ? file.replace(/\s/g, '') : file;
		return path.normalize(path.join(exportDirectory, normalized));
	}

	static getRelativeExport(file) {
		return path.relative(core.view.config.exportDirectory || '', file);
	}

	constructor(count, unit = 'item') {
		this.count = count;
		this.unit = unit;
		this.succeeded = 0;
		this.failed = 0;
		this.lastItem = null;
	}

	get unitFormatted() {
		return this.count === 1 ? this.unit : `${this.unit}s`;
	}

	start() {
		core.view.isBusy++;
		core.view.exportCancelled = false;
	}

	isCancelled() {
		return Boolean(core.view.exportCancelled);
	}

	finish() {
		core.view.isBusy = Math.max(0, core.view.isBusy - 1);
	}

	mark(item, ok = true) {
		this.lastItem = item;
		if (ok) this.succeeded++;
		else this.failed++;
	}

	setCurrentTask() {}
	setCurrentTaskProgress() {}
}

module.exports = ExportHelper;
