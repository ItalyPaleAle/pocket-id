package geolite

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestServiceGetLocationByIPPrivateRanges(t *testing.T) {
	// Private addresses are resolved without reaching the actor, so the service works even with no database around
	svc := newService(nil)

	tests := []struct {
		name      string
		ipAddress string
		country   string
		city      string
	}{
		{name: "empty address", ipAddress: ""},
		{name: "private LAN IPv4", ipAddress: "192.168.1.20", country: internalNetworkCountry, city: "LAN"},
		{name: "private LAN IPv4 in the 10/8 range", ipAddress: "10.4.5.6", country: internalNetworkCountry, city: "LAN"},
		{name: "Tailscale IPv4", ipAddress: "100.101.102.103", country: internalNetworkCountry, city: "Tailscale"},
		{name: "IPv6 unique local address", ipAddress: "fd00::1", country: internalNetworkCountry, city: "LAN"},
		{name: "IPv4 loopback", ipAddress: "127.0.0.1", country: internalNetworkCountry, city: "LAN"},
		{name: "IPv6 loopback", ipAddress: "::1", country: internalNetworkCountry, city: "LAN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			country, city, err := svc.GetLocationByIP(t.Context(), tt.ipAddress)
			require.NoError(t, err)
			require.Equal(t, tt.country, country)
			require.Equal(t, tt.city, city)
		})
	}
}

func TestServiceGetLocationByIPInvalidAddress(t *testing.T) {
	svc := newService(nil)

	_, _, err := svc.GetLocationByIP(t.Context(), "not-an-ip")
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to parse IP address")
}

func TestServiceGetLocationByIP(t *testing.T) {
	actorSvc := newActorServiceForTest(t, actorOptions{updaterDisabled: true})
	storeDatabaseForTest(t, actorSvc, readTestDatabase(t), time.Now())

	svc := newService(actorSvc)

	// The reader has just activated, so the first lookup is the one that loads the database in: the service falls back from Peek to Invoke on its own
	require.Equal(t, lookupStatusNotLoaded, peekLookupForTest(t, actorSvc, "81.2.69.142").Status)

	tests := []struct {
		name      string
		ipAddress string
		country   string
		city      string
	}{
		{name: "public IPv4", ipAddress: "81.2.69.142", country: "United Kingdom", city: "London"},
		{name: "public IPv4 in another country", ipAddress: "216.160.83.56", country: "United States", city: "Milton"},
		{name: "public IPv6", ipAddress: "2001:218::1", country: "Japan"},
		{name: "public address not in the database", ipAddress: "8.8.8.8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			country, city, err := svc.GetLocationByIP(t.Context(), tt.ipAddress)
			require.NoError(t, err)
			require.Equal(t, tt.country, country)
			require.Equal(t, tt.city, city)
		})
	}

	// The fallback only happens until the reader has the database in memory, after which Peek answers on its own
	require.Equal(t, lookupStatusOK, peekLookupForTest(t, actorSvc, "81.2.69.142").Status)
}

func TestServiceGetLocationByIPWithoutDatabase(t *testing.T) {
	// Deployments without a MaxMind license key never download a database: lookups return no location instead of failing
	actorSvc := newActorServiceForTest(t, actorOptions{updaterDisabled: true})
	svc := newService(actorSvc)

	country, city, err := svc.GetLocationByIP(t.Context(), "81.2.69.142")
	require.NoError(t, err)
	require.Empty(t, country)
	require.Empty(t, city)
}
