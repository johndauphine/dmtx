package secrets

import (
	"bytes"
	"errors"
)

// withMasterKey changes only encryption.master_key in the existing text. It
// intentionally does not parse and re-marshal YAML: comments and unknown
// sections must remain byte-for-byte intact.
func withMasterKey(data []byte, key string) ([]byte, error) {
	if bytes.ContainsRune([]byte(key), '\n') || bytes.ContainsRune([]byte(key), '\r') || bytes.ContainsRune([]byte(key), '"') {
		return nil, errors.New("invalid profile master key")
	}
	lines := bytes.SplitAfter(data, []byte("\n"))
	encryption, indent := -1, []byte(nil)
	for index, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if encryption >= 0 && len(bytes.TrimSpace(line)) > 0 && len(line)-len(bytes.TrimLeft(line, " \t")) <= len(indent) {
			break
		}
		if bytes.Equal(trimmed, []byte("encryption:")) {
			encryption = index
			indent = line[:len(line)-len(bytes.TrimLeft(line, " \t"))]
			continue
		}
		if encryption >= 0 && len(line)-len(bytes.TrimLeft(line, " \t")) > len(indent) && bytes.HasPrefix(bytes.TrimLeft(line, " \t"), []byte("master_key:")) {
			prefix := line[:len(line)-len(bytes.TrimLeft(line, " \t"))]
			lines[index] = append(append([]byte{}, prefix...), append([]byte("master_key: \""+key+"\"\n"), nil...)...)
			return bytes.Join(lines, nil), nil
		}
	}
	if encryption < 0 {
		return nil, errors.New("missing encryption section")
	}
	insert := append(append([]byte{}, indent...), []byte("  master_key: \""+key+"\"\n")...)
	lines = append(lines[:encryption+1], append([][]byte{insert}, lines[encryption+1:]...)...)
	return bytes.Join(lines, nil), nil
}
