//go:build linux

package bootstrap

import (
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

var sqliteStatfs = syscall.Statfs

func isSqliteDatabaseOnNetworkFilesystem(dbPath string) (bool, error) {
	var statfs syscall.Statfs_t
	err := sqliteStatfs(filepath.Dir(dbPath), &statfs)
	if err != nil {
		return false, err
	}

	switch int64(statfs.Type) {
	case unix.NFS_SUPER_MAGIC, unix.SMB_SUPER_MAGIC, unix.CIFS_SUPER_MAGIC, unix.FUSE_SUPER_MAGIC:
		return true, nil
	default:
		return false, nil
	}
}
