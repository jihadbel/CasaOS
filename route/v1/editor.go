package v1

import (
	"errors"
	"io"
	"os"

	"github.com/IceWhaleTech/CasaOS/internal/textfile"
)

const maxEditorFileBytes = textfile.MaxEditorFileBytes
const maxEditorRequestBytes = textfile.MaxEditorRequestBytes

var errEditorTooLarge = errors.New("editor supports files up to 16 MiB; download larger files to edit them locally")

func readEditorFile(path string) ([]byte, error) {
	initial, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !initial.Mode().IsRegular() {
		return nil, errors.New("only regular files can be edited")
	}
	if initial.Size() > maxEditorFileBytes {
		return nil, errEditorTooLarge
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("only regular files can be edited")
	}
	if info.Size() > maxEditorFileBytes {
		return nil, errEditorTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(f, maxEditorFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxEditorFileBytes {
		return nil, errEditorTooLarge
	}
	return data, nil
}
