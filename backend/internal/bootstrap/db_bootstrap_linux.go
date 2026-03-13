//go:build linux

package bootstrap

import (
	"path/filepath"
	"syscall"
)

var sqliteStatfs = syscall.Statfs

const (
	nfsSuperMagic  = 0x6969
	smbSuperMagic  = 0x517b
	cifsSuperMagic = 0xff534d42
	fuseSuperMagic = 0x65735546
)

func isSqliteDatabaseOnNetworkFilesystem(dbPath string) (bool, error) {
	var statfs syscall.Statfs_t
	err := sqliteStatfs(filepath.Dir(dbPath), &statfs)
	if err != nil {
		return false, err
	}

	// Statfs_t.Type is arch-dependent (for example, int32 on some systems and int64 on others).
	// Normalize through uint32 first so signed values still preserve the Linux bit pattern for
	// magic numbers such as CIFS (0xff534d42), then compare in a wide unsigned form.
	switch uint64(uint32(statfs.Type)) {
	case nfsSuperMagic, smbSuperMagic, cifsSuperMagic, fuseSuperMagic:
		return true, nil
	default:
		return false, nil
	}
}
