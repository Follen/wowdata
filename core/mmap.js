const fs = require('fs');
const log = require('./log');

const create_virtual_file = () => {
	return {
		isMapped: false,
		data: null,
		lastError: null,
		mapFile: function(filePath) {
			try {
				this.data = fs.readFileSync(filePath);
				this.isMapped = true;
				return true;
			} catch (e) {
				this.lastError = e.message;
				return false;
			}
		},
		unmap: function() { this.data = null; this.isMapped = false; }
	};
};

const release_virtual_files = () => {
	log.write('Simulated release of virtual files (mmap stub)');
};

module.exports = {
	create_virtual_file,
	release_virtual_files
};
