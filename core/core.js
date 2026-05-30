const EventEmitter = require('events');

const events = new EventEmitter();

const makeNewView = () => ({
	isBusy: 0,
	isLoading: false,
	loadPct: -1,
	loadingTitle: '',
	loadingProgress: '',
	toast: null,
	cacheSize: 0,
	exportCancelled: false,
	selectedCDNRegion: { tag: 'cn' },
	config: {},
	casc: null,
	$watch(property, callback, options = {}) {
		if (options.immediate && property === 'config.cascLocale')
			callback(this.config.cascLocale);

		return () => {};
	}
});

const core = {
	events,
	view: null,
	makeNewView,
	create_busy_lock() {
		core.view.isBusy++;
		return { [Symbol.dispose]: () => core.view.isBusy-- };
	},
	showLoadingScreen(segments = 1, title = 'Loading, please wait...') {
		core.view.loadingSegments = segments;
		core.view.loadingProgressValue = 0;
		core.view.loadPct = 0;
		core.view.loadingTitle = title;
		core.view.isLoading = true;
		core.view.isBusy++;
	},
	async progressLoadingScreen(text) {
		core.view.loadingProgressValue = (core.view.loadingProgressValue || 0) + 1;
		core.view.loadPct = Math.min(core.view.loadingProgressValue / (core.view.loadingSegments || 1), 1);
		if (text)
			core.view.loadingProgress = text;
	},
	hideLoadingScreen() {
		core.view.loadPct = -1;
		core.view.isLoading = false;
		core.view.isBusy = Math.max(0, core.view.isBusy - 1);
	},
	setToast(type, message, options = null, ttl = 10000, closable = true) {
		core.view.toast = { type, message, options, ttl, closable };
	},
	hideToast(userCancel = false) {
		core.view.toast = null;
		if (userCancel)
			events.emit('toast-cancelled');
	},
	registerDropHandler() {},
	getDropHandler() { return null; },
	openLastExportStream() { return null; },
	saveScrollPosition() {},
	getScrollPosition() { return null; },
	openExportDirectory() {}
};

core.view = makeNewView();

module.exports = core;
