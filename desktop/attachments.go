package desktop

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const maxAttachmentSize = 50 << 20

func readDroppedAttachment(path string) (string, []byte, error) {
	filename := filepath.Base(path)
	if path == "" || filename == "." || filename == string(filepath.Separator) {
		return "", nil, fmt.Errorf("invalid dropped file path %q", path)
	}

	pathInfo, err := os.Stat(path)
	if err != nil {
		return "", nil, fmt.Errorf("inspect dropped file %q: %w", filename, err)
	}
	if err = validateDroppedFileInfo(pathInfo, filename); err != nil {
		return "", nil, err
	}

	file, err := os.Open(path)
	if err != nil {
		return "", nil, fmt.Errorf("open dropped file %q: %w", filename, err)
	}
	defer file.Close()

	openedInfo, err := file.Stat()
	if err != nil {
		return "", nil, fmt.Errorf("inspect dropped file %q: %w", filename, err)
	}
	if err = validateDroppedFileInfo(openedInfo, filename); err != nil {
		return "", nil, err
	}

	data, err := io.ReadAll(io.LimitReader(file, maxAttachmentSize+1))
	if err != nil {
		return "", nil, fmt.Errorf("read dropped file %q: %w", filename, err)
	}
	if int64(len(data)) > maxAttachmentSize {
		return "", nil, fmt.Errorf("dropped file %q exceeds the 50 MiB limit", filename)
	}
	return filename, data, nil
}

func validateDroppedFileInfo(info os.FileInfo, filename string) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("dropped file %q is not a regular file", filename)
	}
	if info.Size() > maxAttachmentSize {
		return fmt.Errorf("dropped file %q exceeds the 50 MiB limit", filename)
	}
	return nil
}
