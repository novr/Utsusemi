package doctor

import (
	"fmt"
	"strings"
)

func FormatText(r Report) string {
	var b strings.Builder
	for _, c := range r.Checks {
		fmt.Fprintf(&b, "[%s] %s: %s\n", c.Status, c.Name, c.Message)
	}
	return b.String()
}
