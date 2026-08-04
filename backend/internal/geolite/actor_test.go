package geolite

import (
	"sync"
	"testing"
	"time"

	"github.com/italypaleale/francis/actor"
	"github.com/stretchr/testify/require"
)

func TestReaderActorLookup(t *testing.T) {
	svc := newActorServiceForTest(t, actorOptions{updaterDisabled: true})
	storeDatabaseForTest(t, svc, readTestDatabase(t), time.Now())

	// The first lookup goes through Invoke, which is what loads the database into the freshly activated reader
	require.Equal(t, lookupStatusOK, invokeLookupForTest(t, svc, "81.2.69.142").Status)

	tests := []struct {
		name      string
		ipAddress string
		country   string
		city      string
	}{
		{name: "IPv4 with country and city", ipAddress: "81.2.69.142", country: "United Kingdom", city: "London"},
		{name: "IPv4 in another country", ipAddress: "89.160.20.112", country: "Sweden", city: "Linköping"},
		{name: "IPv4 with country only", ipAddress: "67.43.156.1", country: "Bhutan"},
		{name: "IPv6", ipAddress: "2001:218::1", country: "Japan"},
		{name: "address not in the database", ipAddress: "8.8.8.8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := peekLookupForTest(t, svc, tt.ipAddress)
			require.Equal(t, lookupStatusOK, res.Status)
			require.Equal(t, tt.country, res.Country)
			require.Equal(t, tt.city, res.City)
		})
	}
}

func TestReaderActorLookupBeforeLoad(t *testing.T) {
	// A Peek can't read the database into a freshly activated reader, since that would mutate the actor: it reports that instead, so the caller comes back through Invoke
	svc := newActorServiceForTest(t, actorOptions{updaterDisabled: true})
	storeDatabaseForTest(t, svc, readTestDatabase(t), time.Now())

	res := peekLookupForTest(t, svc, "81.2.69.142")
	require.Equal(t, lookupStatusNotLoaded, res.Status)
	require.Empty(t, res.Country)

	res = invokeLookupForTest(t, svc, "81.2.69.142")
	require.Equal(t, lookupStatusOK, res.Status)
	require.Equal(t, "United Kingdom", res.Country)

	// Once loaded, Peek serves the lookups
	res = peekLookupForTest(t, svc, "81.2.69.142")
	require.Equal(t, lookupStatusOK, res.Status)
	require.Equal(t, "United Kingdom", res.Country)
}

func TestReaderActorLookupWithoutDatabase(t *testing.T) {
	// No database has been downloaded yet, so lookups return an empty result rather than failing
	svc := newActorServiceForTest(t, actorOptions{updaterDisabled: true})

	res := invokeLookupForTest(t, svc, "81.2.69.142")
	require.Equal(t, lookupStatusOK, res.Status)
	require.Empty(t, res.Country)
	require.Empty(t, res.City)

	// The reader now knows the state store holds no database, so it doesn't ask a caller to come back through Invoke
	res = peekLookupForTest(t, svc, "81.2.69.142")
	require.Equal(t, lookupStatusOK, res.Status)
	require.Empty(t, res.Country)
}

func TestReaderActorLookupInvalidAddress(t *testing.T) {
	svc := newActorServiceForTest(t, actorOptions{updaterDisabled: true})
	storeDatabaseForTest(t, svc, readTestDatabase(t), time.Now())

	_, err := svc.Invoke(t.Context(), ActorType, actor.SingletonActorID, methodLookup, lookupRequest{IPAddress: "not-an-ip"})
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to parse IP address")
}

func TestReaderActorLookupUnsupportedMethod(t *testing.T) {
	svc := newActorServiceForTest(t, actorOptions{updaterDisabled: true})

	_, err := svc.Peek(t.Context(), ActorType, actor.SingletonActorID, "unsupported", nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "unsupported")
}

func TestReaderActorConcurrentLookups(t *testing.T) {
	// Lookups are served through Peek, which runs under the actor's shared lock, so they don't serialize on each other
	svc := newActorServiceForTest(t, actorOptions{updaterDisabled: true})
	storeDatabaseForTest(t, svc, readTestDatabase(t), time.Now())
	require.Equal(t, lookupStatusOK, invokeLookupForTest(t, svc, "81.2.69.142").Status)

	const concurrency = 25

	// Release every goroutine at once, so the lookups overlap on the actor
	start := make(chan struct{})
	results := make([]lookupResponse, concurrency)
	errs := make([]error, concurrency)

	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := range concurrency {
		go func() {
			defer wg.Done()
			<-start
			results[i], errs[i] = lookupForTest(t.Context(), svc, false, "81.2.69.142")
		}()
	}

	close(start)
	wg.Wait()

	for i, res := range results {
		require.NoError(t, errs[i])
		require.Equal(t, lookupStatusOK, res.Status)
		require.Equal(t, "United Kingdom", res.Country)
		require.Equal(t, "London", res.City)
	}
}

func TestReaderActorReload(t *testing.T) {
	svc := newActorServiceForTest(t, actorOptions{updaterDisabled: true})

	// The reader reads the state store once, finds no database there, and keeps serving lookups from that
	res := invokeLookupForTest(t, svc, "81.2.69.142")
	require.Empty(t, res.Country)

	// Storing a database isn't enough on its own: the reader still answers from what it has in memory
	storeDatabaseForTest(t, svc, readTestDatabase(t), time.Now())
	res = peekLookupForTest(t, svc, "81.2.69.142")
	require.Equal(t, lookupStatusOK, res.Status)
	require.Empty(t, res.Country)

	// The reload the updater performs after a download makes the reader pick it up
	_, err := svc.Invoke(t.Context(), ActorType, actor.SingletonActorID, methodReload, nil)
	require.NoError(t, err)

	res = peekLookupForTest(t, svc, "81.2.69.142")
	require.Equal(t, "United Kingdom", res.Country)
	require.Equal(t, "London", res.City)
}

func TestReaderActorReloadKeepsDatabaseOnInvalidState(t *testing.T) {
	svc := newActorServiceForTest(t, actorOptions{updaterDisabled: true})
	storeDatabaseForTest(t, svc, readTestDatabase(t), time.Now())

	res := invokeLookupForTest(t, svc, "81.2.69.142")
	require.Equal(t, "United Kingdom", res.Country)

	// A corrupted database in the state store fails the reload, and the database already in memory keeps serving lookups
	storeDatabaseForTest(t, svc, []byte("not a database"), time.Now().Add(time.Minute))

	_, err := svc.Invoke(t.Context(), ActorType, actor.SingletonActorID, methodReload, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "error opening the GeoLite2 City database")

	res = peekLookupForTest(t, svc, "81.2.69.142")
	require.Equal(t, "United Kingdom", res.Country)
}

func TestUpdaterActorUpdate(t *testing.T) {
	database := readTestDatabase(t)
	archive := buildTarGzForTest(t, map[string][]byte{
		"GeoLite2-City_20260101/COPYRIGHT.txt":       []byte("copyright"),
		"GeoLite2-City_20260101/" + databaseFileName: database,
	})
	httpClient, transport := newDownloadClientForTest(archive)

	host := newActorHostForTest(t, actorOptions{
		httpClient:  httpClient,
		downloadURL: testDownloadURL,
	})
	svc := host.Service()

	_, err := svc.Invoke(t.Context(), ActorType, updaterActorID, methodUpdate, nil)
	require.NoError(t, err)
	require.Equal(t, int32(1), transport.requests.Load())

	// The database is stored for the reader, and never written to disk
	var state databaseState
	err = svc.GetState(t.Context(), ActorType, actor.SingletonActorID, &state)
	require.NoError(t, err)
	require.Equal(t, database, state.Data)
	require.WithinDuration(t, time.Now(), state.UpdatedAt, time.Minute)

	// The updater asks the reader to reload, so lookups resolve right away, without a caller having to load the database in first
	res := peekLookupForTest(t, svc, "81.2.69.142")
	require.Equal(t, lookupStatusOK, res.Status)
	require.Equal(t, "United Kingdom", res.Country)
	require.Equal(t, "London", res.City)

	// The next update is scheduled for when this database goes stale
	requireUpdateScheduledForTest(t, host, state.UpdatedAt.Add(databaseMaxAge))
}

func TestUpdaterActorSkipsRecentDatabase(t *testing.T) {
	database := readTestDatabase(t)
	archive := buildTarGzForTest(t, map[string][]byte{"GeoLite2-City_20260101/" + databaseFileName: database})
	httpClient, transport := newDownloadClientForTest(archive)

	host := newActorHostForTest(t, actorOptions{
		httpClient:  httpClient,
		downloadURL: testDownloadURL,
	})
	svc := host.Service()

	// The first update downloads the database
	_, err := svc.Invoke(t.Context(), ActorType, updaterActorID, methodUpdate, nil)
	require.NoError(t, err)
	require.Equal(t, int32(1), transport.requests.Load())

	// The second one finds it recent enough and doesn't download it again, but still re-arms the alarm for when the database goes stale
	_, err = svc.Invoke(t.Context(), ActorType, updaterActorID, methodUpdate, nil)
	require.NoError(t, err)
	require.Equal(t, int32(1), transport.requests.Load())

	var state updaterState
	err = svc.GetState(t.Context(), ActorType, updaterActorID, &state)
	require.NoError(t, err)
	requireUpdateScheduledForTest(t, host, state.UpdatedAt.Add(databaseMaxAge))
}

func TestUpdaterActorUpdatesStaleDatabase(t *testing.T) {
	database := readTestDatabase(t)
	archive := buildTarGzForTest(t, map[string][]byte{"GeoLite2-City_20260101/" + databaseFileName: database})
	httpClient, transport := newDownloadClientForTest(archive)

	svc := newActorServiceForTest(t, actorOptions{
		httpClient:  httpClient,
		downloadURL: testDownloadURL,
	})

	// Pretend the database was downloaded longer ago than databaseMaxAge
	err := svc.SetState(t.Context(), ActorType, updaterActorID, updaterState{
		UpdatedAt: time.Now().Add(-databaseMaxAge - time.Hour),
	}, nil)
	require.NoError(t, err)

	_, err = svc.Invoke(t.Context(), ActorType, updaterActorID, methodUpdate, nil)
	require.NoError(t, err)
	require.Equal(t, int32(1), transport.requests.Load())
}

func TestUpdaterActorDownloadFailureSchedulesRetry(t *testing.T) {
	httpClient, transport := newDownloadClientForTest(nil)
	transport.statusCode = 500

	host := newActorHostForTest(t, actorOptions{
		httpClient:  httpClient,
		downloadURL: testDownloadURL,
	})
	svc := host.Service()

	_, err := svc.Invoke(t.Context(), ActorType, updaterActorID, methodUpdate, nil)
	require.NoError(t, err)
	require.Equal(t, int32(1), transport.requests.Load())

	// A failed download leaves no trace, so the retry starts over
	var state updaterState
	err = svc.GetState(t.Context(), ActorType, updaterActorID, &state)
	require.ErrorIs(t, err, actor.ErrStateNotFound)

	// The alarm is re-armed for a retry rather than left to the framework, which would delete it once the attempts run out
	requireUpdateScheduledForTest(t, host, time.Now().Add(updateRetryInterval))
}

func TestUpdaterActorUnsupportedMethod(t *testing.T) {
	svc := newActorServiceForTest(t, actorOptions{})

	_, err := svc.Invoke(t.Context(), ActorType, updaterActorID, "unsupported", nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "unsupported")
}

func TestUpdaterActorAlarm(t *testing.T) {
	// The alarm is what drives updates in the cluster, replacing the scheduled job that used to refresh the database
	database := readTestDatabase(t)
	archive := buildTarGzForTest(t, map[string][]byte{"GeoLite2-City_20260101/" + databaseFileName: database})
	httpClient, transport := newDownloadClientForTest(archive)

	host := newActorHostForTest(t, actorOptions{
		httpClient:  httpClient,
		downloadURL: testDownloadURL,
	})
	svc := host.Service()

	err := svc.SetAlarm(t.Context(), ActorType, updaterActorID, alarmUpdate, actor.AlarmProperties{DueTime: time.Now()})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return transport.requests.Load() > 0
	}, 30*time.Second, 100*time.Millisecond, "the update alarm never fired")

	require.Eventually(t, func() bool {
		res, err := lookupForTest(t.Context(), svc, false, "81.2.69.142")
		return err == nil && res.Country == "United Kingdom"
	}, 30*time.Second, 100*time.Millisecond, "the reader never picked up the downloaded database")

	// The alarm doesn't repeat on a fixed interval: the run that just completed left the next one scheduled for when the database it downloaded goes stale
	var state updaterState
	err = svc.GetState(t.Context(), ActorType, updaterActorID, &state)
	require.NoError(t, err)
	requireUpdateScheduledForTest(t, host, state.UpdatedAt.Add(databaseMaxAge))

	// It doesn't download again in the meantime
	require.Never(t, func() bool {
		return transport.requests.Load() > 1
	}, 3*time.Second, 250*time.Millisecond, "the database was downloaded again before it went stale")
}
