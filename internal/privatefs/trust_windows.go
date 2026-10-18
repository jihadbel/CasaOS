package privatefs

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var securityAPI = windows.NewLazySystemDLL("advapi32.dll")
var getACLInformation = securityAPI.NewProc("GetAclInformation")
var getACE = securityAPI.NewProc("GetAce")

const trustedInstallerSID = "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"

func trustedSID(sid *windows.SID, ancestor bool) bool {
	if sid == nil {
		return false
	}
	text := sid.String()
	if text == "S-1-5-18" || text == "S-1-5-32-544" || ancestor && text == trustedInstallerSID {
		return true
	}
	// An explicitly elevated administrator may migrate their own older install.
	// SYSTEM does not implicitly trust arbitrary local user SIDs.
	token := windows.GetCurrentProcessToken()
	if token.IsElevated() {
		user, err := token.GetTokenUser()
		return err == nil && text == user.User.Sid.String()
	}
	return false
}

func checkTrusted(path string, ancestor bool) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	if err := checkDescriptor(sd, ancestor); err != nil {
		return fmt.Errorf("untrusted service storage at %s (review ownership and write permissions before migration): %w", path, err)
	}
	return nil
}

func checkDescriptor(sd *windows.SECURITY_DESCRIPTOR, ancestor bool) error {
	defer runtime.KeepAlive(sd)
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	if !trustedSID(owner, ancestor) {
		return errors.New("owner is not a trusted service administrator")
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if acl == nil {
		return errors.New("missing or null DACL permits unrestricted access")
	}
	// ACL_SIZE_INFORMATION, requested with AclSizeInformation (2).
	var info struct{ Count, InUse, Free uint32 }
	if ok, _, callErr := getACLInformation.Call(uintptr(unsafe.Pointer(acl)), uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info), 2); ok == 0 {
		return callErr
	}
	const deleteChild = 0x40
	forbidden := uint32(windows.GENERIC_ALL | windows.DELETE | windows.WRITE_DAC | windows.WRITE_OWNER | deleteChild)
	if !ancestor {
		forbidden |= windows.GENERIC_WRITE | windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA | windows.FILE_WRITE_EA | windows.FILE_WRITE_ATTRIBUTES
	}
	for index := uint32(0); index < info.Count; index++ {
		var ace unsafe.Pointer
		if ok, _, callErr := getACE.Call(uintptr(unsafe.Pointer(acl)), uintptr(index), uintptr(unsafe.Pointer(&ace))); ok == 0 {
			return callErr
		}
		// ACCESS_ALLOWED_ACE: ACE_HEADER, ACCESS_MASK, then the trustee SID.
		header := (*struct {
			Type, Flags byte
			Size        uint16
			Mask        uint32
		})(ace)
		if ancestor && header.Flags&windows.INHERIT_ONLY_ACE != 0 {
			continue
		}
		if header.Type == 1 { // ACCESS_DENIED_ACE cannot grant write access.
			continue
		}
		if header.Type != 0 || header.Size < 16 {
			return errors.New("unsupported service DACL entry")
		}
		sid := (*windows.SID)(unsafe.Add(ace, 8))
		if !trustedSID(sid, ancestor) && header.Mask&forbidden != 0 {
			return errors.New("an untrusted account can modify or replace service data")
		}
	}
	return nil
}

func createPrivateDirectory(path string) error {
	sd, err := windows.SecurityDescriptorFromString(directoryACL)
	if err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	sa := windows.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(sa))
	// Set owner and the protected DACL at creation, before any other process can
	// place configuration or credentials in an inherited writable directory.
	err = windows.CreateDirectory(name, &sa)
	if err != nil {
		return &os.PathError{Op: "CreateDirectory", Path: path, Err: err}
	}
	return nil
}
