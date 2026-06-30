package agent

import "os"

func removeFile(path string) error {
	return os.Remove(path)
}
