package runtime

import (
	"io"

	"wowdata/internal/casc"
)

func cascDownloadProgressWriter() io.Writer {
	return casc.DownloadProgressWriterForTest()
}

func setCASCDownloadProgressWriter(w io.Writer) {
	casc.SetDownloadProgressWriterForTest(w)
}
