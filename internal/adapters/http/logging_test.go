package http

import (
	"bytes"
	"strings"
)

func errorLogs(logs *bytes.Buffer) string {
	var out strings.Builder
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.Contains(line, `"level":"ERROR"`) {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	return out.String()
}
