package localauth

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/IceWhaleTech/CasaOS/internal/privatefs"
	"github.com/IceWhaleTech/CasaOS/internal/safefs"
)

// NewWithStorage is the production standalone entry point. Failure to apply
// operating-system protection stops startup; there is no unprotected fallback.
func NewWithStorage(dir, root string) (*Manager, error) {
	if !filepath.IsAbs(root) || filepath.Dir(root) == root {
		return nil, errors.New("invalid private storage root")
	}
	serviceBase := filepath.Dir(filepath.Dir(dir))
	if !safefs.Within(serviceBase, root) || safefs.Within(dir, root) {
		return nil, errors.New("private storage must be a dedicated sibling of the service configuration directory")
	}
	if err := privatefs.ProtectTree(serviceBase); err != nil {
		return nil, err
	}
	if err := privatefs.ProtectDirectory(root); err != nil {
		return nil, err
	}
	m, err := newManager(dir, privatefs.ProtectDirectory)
	if err != nil {
		return nil, err
	}
	m.storageRoot = filepath.Clean(root)
	m.protectHome = privatefs.ProtectDirectory
	for id := range m.users {
		if err := m.prepareHome(id); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (m *Manager) prepareHome(id string) error {
	root := filepath.Join(m.storageRoot, id)
	if !safefs.Within(m.storageRoot, root) {
		return errors.New("invalid user home")
	}
	if err := safefs.NoLinks(root); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	if m.protectHome != nil {
		if err := m.protectHome(root); err != nil {
			return err
		}
	}
	for _, name := range []string{"Photos", "Videos", "Documents", "Music", "Applications", "Archives", "Mobile_Uploads"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0700); err != nil {
			return err
		}
	}
	return nil
}
