//go:build !linux

package bootstrap

func isSqliteDatabaseOnNetworkFilesystem(string) (bool, error) {
	return false, nil
}
