package envfile

import (
	"fmt"
	"os"
	"strings"
)

// Upsert sets KEY=value in a dotenv-style file, creating the file if needed.
func Upsert(path, key, value string) error {
	if path == "" || key == "" {
		return fmt.Errorf("env file path and key required")
	}
	content := ""
	data, err := os.ReadFile(path)
	if err == nil {
		content = string(data)
	} else if !os.IsNotExist(err) {
		return err
	}

	line := key + "=" + value
	prefix := key + "="
	lines := strings.Split(content, "\n")
	found := false
	for i, existing := range lines {
		trimmed := strings.TrimSpace(existing)
		if strings.HasPrefix(trimmed, prefix) {
			lines[i] = line
			found = true
			break
		}
	}
	if !found {
		if content != "" && !strings.HasSuffix(content, "\n") {
			lines = append(lines, line)
		} else if content == "" {
			lines = []string{line}
		} else {
			// content ends with newline → last split element is empty
			if len(lines) > 0 && lines[len(lines)-1] == "" {
				lines[len(lines)-1] = line
				lines = append(lines, "")
			} else {
				lines = append(lines, line)
			}
		}
	}
	out := strings.Join(lines, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return os.WriteFile(path, []byte(out), 0o600)
}
