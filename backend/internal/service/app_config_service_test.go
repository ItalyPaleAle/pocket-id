package service

import (
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/pocket-id/pocket-id/backend/internal/model"
	"github.com/pocket-id/pocket-id/backend/internal/utils"
	"github.com/stretchr/testify/require"
)

// NewTestAppConfigService is a function used by tests to create AppConfigService objects with pre-defined configuration values
func NewTestAppConfigService(config *model.AppConfig) *AppConfigService {
	service := &AppConfigService{
		dbConfig: atomic.Pointer[model.AppConfig]{},
	}
	service.dbConfig.Store(config)

	return service
}

func TestLoadDbConfig(t *testing.T) {
	t.Run("empty config table", func(t *testing.T) {
		db := newAppConfigTestDatabaseForTest(t)
		service := &AppConfigService{
			db: db,
		}

		// Load the config
		err := service.LoadDbConfig(t.Context())
		require.NoError(t, err)

		// Config should be equal to default config
		require.EqualValues(t, service.GetDbConfig(), service.getDefaultDbConfig())
	})

	t.Run("loads value from config table", func(t *testing.T) {
		db := newAppConfigTestDatabaseForTest(t)

		// Populate the config table with some initial values
		err := db.
			Create([]model.AppConfigVariable{
				// Should be set to the default value because it's an empty string
				{Key: "appName", Value: ""},
				// Overrides default value
				{Key: "sessionDuration", Value: "5"},
				// Does not have a default value
				{Key: "smtpHost", Value: "example"},
			}).
			Error
		require.NoError(t, err)

		// Load the config
		service := &AppConfigService{
			db: db,
		}
		err = service.LoadDbConfig(t.Context())
		require.NoError(t, err)

		// Values should match expected ones
		expect := service.getDefaultDbConfig()
		expect.SessionDuration.Value = "5"
		expect.SmtpHost.Value = "example"
		require.EqualValues(t, service.GetDbConfig(), expect)
	})

	t.Run("ignores unknown config keys", func(t *testing.T) {
		db := newAppConfigTestDatabaseForTest(t)

		// Add an entry with a key that doesn't exist in the config struct
		err := db.Create([]model.AppConfigVariable{
			{Key: "__nonExistentKey", Value: "some value"},
			{Key: "appName", Value: "TestApp"}, // This one should still be loaded
		}).Error
		require.NoError(t, err)

		service := &AppConfigService{
			db: db,
		}
		// This should not fail, just ignore the unknown key
		err = service.LoadDbConfig(t.Context())
		require.NoError(t, err)

		config := service.GetDbConfig()
		require.Equal(t, "TestApp", config.AppName.Value)
	})

	t.Run("loading config multiple times", func(t *testing.T) {
		db := newAppConfigTestDatabaseForTest(t)

		// Initial state
		err := db.Create([]model.AppConfigVariable{
			{Key: "appName", Value: "InitialApp"},
		}).Error
		require.NoError(t, err)

		service := &AppConfigService{
			db: db,
		}
		err = service.LoadDbConfig(t.Context())
		require.NoError(t, err)
		require.Equal(t, "InitialApp", service.GetDbConfig().AppName.Value)

		// Update the database value
		err = db.Model(&model.AppConfigVariable{}).
			Where("key = ?", "appName").
			Update("value", "UpdatedApp").Error
		require.NoError(t, err)

		// Load the config again, it should reflect the updated value
		err = service.LoadDbConfig(t.Context())
		require.NoError(t, err)
		require.Equal(t, "UpdatedApp", service.GetDbConfig().AppName.Value)
	})
}

func TestUpdateAppConfigValues(t *testing.T) {
	t.Run("update single value", func(t *testing.T) {
		db := newAppConfigTestDatabaseForTest(t)

		// Create a service with default config
		service := &AppConfigService{
			db: db,
		}
		err := service.LoadDbConfig(t.Context())
		require.NoError(t, err)

		// Update a single config value
		err = service.UpdateAppConfigValues(t.Context(), "appName", "Test App")
		require.NoError(t, err)

		// Verify in-memory config was updated
		config := service.GetDbConfig()
		require.Equal(t, "Test App", config.AppName.Value)

		// Verify database was updated
		var dbValue model.AppConfigVariable
		err = db.Where("key = ?", "appName").First(&dbValue).Error
		require.NoError(t, err)
		require.Equal(t, "Test App", dbValue.Value)
	})

	t.Run("update multiple values", func(t *testing.T) {
		db := newAppConfigTestDatabaseForTest(t)

		// Create a service with default config
		service := &AppConfigService{
			db: db,
		}
		err := service.LoadDbConfig(t.Context())
		require.NoError(t, err)

		// Update multiple config values
		err = service.UpdateAppConfigValues(
			t.Context(),
			"appName", "Test App",
			"sessionDuration", "30",
			"smtpHost", "mail.example.com",
		)
		require.NoError(t, err)

		// Verify in-memory config was updated
		config := service.GetDbConfig()
		require.Equal(t, "Test App", config.AppName.Value)
		require.Equal(t, "30", config.SessionDuration.Value)
		require.Equal(t, "mail.example.com", config.SmtpHost.Value)

		// Verify database was updated
		var count int64
		db.Model(&model.AppConfigVariable{}).Count(&count)
		require.Equal(t, int64(3), count)

		var appName, sessionDuration, smtpHost model.AppConfigVariable
		err = db.Where("key = ?", "appName").First(&appName).Error
		require.NoError(t, err)
		require.Equal(t, "Test App", appName.Value)

		err = db.Where("key = ?", "sessionDuration").First(&sessionDuration).Error
		require.NoError(t, err)
		require.Equal(t, "30", sessionDuration.Value)

		err = db.Where("key = ?", "smtpHost").First(&smtpHost).Error
		require.NoError(t, err)
		require.Equal(t, "mail.example.com", smtpHost.Value)
	})

	t.Run("empty value resets to default", func(t *testing.T) {
		db := newAppConfigTestDatabaseForTest(t)

		// Create a service with default config
		service := &AppConfigService{
			db: db,
		}
		err := service.LoadDbConfig(t.Context())
		require.NoError(t, err)

		// First change the value
		err = service.UpdateAppConfigValues(t.Context(), "sessionDuration", "30")
		require.NoError(t, err)
		require.Equal(t, "30", service.GetDbConfig().SessionDuration.Value)

		// Now set it to empty which should use default value
		err = service.UpdateAppConfigValues(t.Context(), "sessionDuration", "")
		require.NoError(t, err)
		require.Equal(t, "60", service.GetDbConfig().SessionDuration.Value) // Default value from getDefaultDbConfig
	})

	t.Run("error with odd number of arguments", func(t *testing.T) {
		db := newAppConfigTestDatabaseForTest(t)

		// Create a service with default config
		service := &AppConfigService{
			db: db,
		}
		err := service.LoadDbConfig(t.Context())
		require.NoError(t, err)

		// Try to update with odd number of arguments
		err = service.UpdateAppConfigValues(t.Context(), "appName", "Test App", "sessionDuration")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid number of arguments")
	})

	t.Run("error with invalid key", func(t *testing.T) {
		db := newAppConfigTestDatabaseForTest(t)

		// Create a service with default config
		service := &AppConfigService{
			db: db,
		}
		err := service.LoadDbConfig(t.Context())
		require.NoError(t, err)

		// Try to update with invalid key
		err = service.UpdateAppConfigValues(t.Context(), "nonExistentKey", "some value")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid configuration key")
	})
}

// Implements gorm's logger.Writer interface
type testLoggerAdapter struct {
	t *testing.T
}

func (l testLoggerAdapter) Printf(format string, args ...any) {
	l.t.Logf(format, args...)
}

func newAppConfigTestDatabaseForTest(t *testing.T) *gorm.DB {
	t.Helper()

	// Get a name for this in-memory database that is specific to the test
	dbName := utils.CreateSha256Hash(t.Name())

	// Connect to a new in-memory SQL database
	db, err := gorm.Open(
		sqlite.Open("file:"+dbName+"?mode=memory&cache=shared"),
		&gorm.Config{
			TranslateError: true,
			Logger: logger.New(
				testLoggerAdapter{t: t},
				logger.Config{
					SlowThreshold:             200 * time.Millisecond,
					LogLevel:                  logger.Info,
					IgnoreRecordNotFoundError: false,
					ParameterizedQueries:      false,
					Colorful:                  false,
				},
			),
		})
	require.NoError(t, err, "Failed to connect to test database")

	// Create the app_config_variables table
	err = db.Exec(`
CREATE TABLE app_config_variables
(
    key           VARCHAR(100) NOT NULL PRIMARY KEY,
    value         TEXT NOT NULL
)
`).Error
	require.NoError(t, err, "Failed to create test config table")

	return db
}
