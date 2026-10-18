package privatefs

import (
	"errors"
	"path/filepath"

	"golang.org/x/sys/windows"
)

const directoryACL = "O:BAG:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
const fileACL = "O:BAG:BAD:P(A;;FA;;;SY)(A;;FA;;;BA)"

func requireServiceIdentity() error {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return errors.New("protected local storage requires the CasaOS SYSTEM service or an elevated administrator")
	}
	return nil
}

func protect(path string, directory bool) error {
	volume, err := windows.UTF16PtrFromString(filepath.VolumeName(path) + `\`)
	if err != nil {
		return err
	}
	var flags uint32
	if err := windows.GetVolumeInformation(volume, nil, 0, nil, nil, &flags, nil, 0); err != nil {
		return err
	}
	if flags&windows.FILE_PERSISTENT_ACLS == 0 {
		return errors.New("private storage requires a filesystem with persistent Windows ACLs (such as NTFS)")
	}
	sddl := fileACL
	if directory {
		sddl = directoryACL
	}
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, owner, nil, dacl, nil)
}
