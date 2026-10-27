package pull

import "io"

func stderrOf(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}
