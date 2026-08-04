package geolite

import (
	"testing"
	"time"

	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/host/local"
	"github.com/stretchr/testify/require"

	"github.com/pocket-id/pocket-id/backend/internal/common"
	testutils "github.com/pocket-id/pocket-id/backend/internal/utils/testing"
)

// newModuleForTest starts a test actor host with the module registered on it, exactly as the server does at startup, and returns both
func newModuleForTest(t *testing.T, deps Dependencies) (*Module, *local.Host) {
	t.Helper()

	var module *Module
	host := testutils.NewActorHostForTest(t, func(t *testing.T, h *local.Host) {
		var err error
		deps.Actors = h
		module, err = New(deps)
		require.NoError(t, err)
	})
	require.NotNil(t, module)

	return module, host
}

func TestModuleUpdatesDatabaseOnStartup(t *testing.T) {
	// Bootstrapping the singleton actor arms the update alarm, which replaces the scheduled job that used to refresh the database
	database := readTestDatabase(t)
	archive := buildTarGzForTest(t, map[string][]byte{"GeoLite2-City_20260101/" + databaseFileName: database})
	httpClient, transport := newDownloadClientForTest(archive)

	module, host := newModuleForTest(t, Dependencies{
		HTTPClient:  httpClient,
		DownloadURL: testDownloadURL,
		LicenseKey:  "test-license-key",
	})

	require.Eventually(t, func() bool {
		return transport.requests.Load() > 0
	}, 30*time.Second, 100*time.Millisecond, "the database was never downloaded")

	require.Eventually(t, func() bool {
		country, _, err := module.GetLocationByIP(t.Context(), "81.2.69.142")
		return err == nil && country == "United Kingdom"
	}, 30*time.Second, 100*time.Millisecond, "the downloaded database never became available for lookups")

	// The run that just completed left the next update scheduled for when the database it downloaded goes stale
	var state updaterState
	err := host.Service().GetState(t.Context(), ActorType, updaterActorID, &state)
	require.NoError(t, err)
	requireUpdateScheduledForTest(t, host, state.UpdatedAt.Add(databaseMaxAge))

	// Private addresses are still resolved locally
	country, city, err := module.GetLocationByIP(t.Context(), "192.168.1.20")
	require.NoError(t, err)
	require.Equal(t, internalNetworkCountry, country)
	require.Equal(t, "LAN", city)
}

func TestModuleUpdaterDisabled(t *testing.T) {
	// Without a MaxMind license key, and with the default download URL, there's no way to download the database, so no update is scheduled
	httpClient, transport := newDownloadClientForTest(nil)

	module, host := newModuleForTest(t, Dependencies{
		HTTPClient:  httpClient,
		DownloadURL: common.MaxMindGeoLiteCityUrl,
	})

	// Wait for well over the alarm poll interval, to give a scheduled update the chance to run
	time.Sleep(5 * time.Second)
	require.Zero(t, transport.requests.Load(), "the database was downloaded even though the updater is disabled")

	_, err := host.GetAlarm(t.Context(), ActorType, updaterActorID, alarmUpdate)
	require.ErrorIs(t, err, actor.ErrAlarmNotFound)

	// Lookups still work, they just don't resolve a location
	country, city, err := module.GetLocationByIP(t.Context(), "81.2.69.142")
	require.NoError(t, err)
	require.Empty(t, country)
	require.Empty(t, city)
}
