//go:build linux

package bootstrap

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/pocket-id/pocket-id/backend/internal/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ext4SuperMagic = 0xef53

func TestConnectDatabase_DoesNotMarkLocalFilesystem(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "pocket-id.db")

	restoreEnv := setTestEnvConfig(t, common.EnvConfigSchema{
		DbProvider:         common.DbProviderSqlite,
		DbConnectionString: dbPath,
		LogLevel:           "warn",
	})
	defer restoreEnv()

	restoreLogger, logs := captureSlogOutput(t, slog.LevelWarn)
	defer restoreLogger()

	originalStatfs := sqliteStatfs
	sqliteStatfs = func(_ string, statfs *syscall.Statfs_t) error {
		statfs.Type = ext4SuperMagic
		return nil
	}
	t.Cleanup(func() {
		sqliteStatfs = originalStatfs
	})

	db, err := ConnectDatabase()
	require.NoError(t, err)

	assert.False(t, IsSqliteDatabaseOnNetworkFilesystem(db))
	assert.NotContains(t, logs.String(), "⚠️⚠️⚠️")

	sqlDB, err := db.DB()
	require.NoError(t, err)
	assert.NoError(t, sqlDB.Close())
}

func TestConnectDatabase_WarnsAndMarksNetworkFilesystem(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "pocket-id.db")

	restoreEnv := setTestEnvConfig(t, common.EnvConfigSchema{
		DbProvider:         common.DbProviderSqlite,
		DbConnectionString: dbPath,
		LogLevel:           "warn",
	})
	defer restoreEnv()

	restoreLogger, logs := captureSlogOutput(t, slog.LevelWarn)
	defer restoreLogger()

	originalStatfs := sqliteStatfs
	sqliteStatfs = func(_ string, statfs *syscall.Statfs_t) error {
		statfs.Type = nfsSuperMagic
		return nil
	}
	t.Cleanup(func() {
		sqliteStatfs = originalStatfs
	})

	db, err := ConnectDatabase()
	require.NoError(t, err)

	assert.True(t, IsSqliteDatabaseOnNetworkFilesystem(db))
	assert.Contains(t, logs.String(), "⚠️⚠️⚠️ SQLite databases should not be stored on a networked file system like NFS, SMB, or FUSE")

	sqlDB, err := db.DB()
	require.NoError(t, err)
	assert.NoError(t, sqlDB.Close())
}

func TestConnectDatabase_IgnoresFilesystemDetectionErrors(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "pocket-id.db")

	restoreEnv := setTestEnvConfig(t, common.EnvConfigSchema{
		DbProvider:         common.DbProviderSqlite,
		DbConnectionString: dbPath,
		LogLevel:           "warn",
	})
	defer restoreEnv()

	restoreLogger, logs := captureSlogOutput(t, slog.LevelWarn)
	defer restoreLogger()

	originalStatfs := sqliteStatfs
	sqliteStatfs = func(_ string, _ *syscall.Statfs_t) error {
		return os.ErrPermission
	}
	t.Cleanup(func() {
		sqliteStatfs = originalStatfs
	})

	db, err := ConnectDatabase()
	require.NoError(t, err)

	assert.False(t, IsSqliteDatabaseOnNetworkFilesystem(db))
	assert.Contains(t, logs.String(), "Failed to detect filesystem type for the SQLite database directory")

	sqlDB, err := db.DB()
	require.NoError(t, err)
	assert.NoError(t, sqlDB.Close())
}

func setTestEnvConfig(t *testing.T, cfg common.EnvConfigSchema) func() {
	t.Helper()

	original := common.EnvConfig
	common.EnvConfig = cfg

	return func() {
		common.EnvConfig = original
	}
}

func captureSlogOutput(t *testing.T, level slog.Leveler) (func(), *bytes.Buffer) {
	t.Helper()

	buffer := &bytes.Buffer{}
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buffer, &slog.HandlerOptions{
		Level: level,
	})))

	return func() {
		slog.SetDefault(original)
	}, buffer
}
