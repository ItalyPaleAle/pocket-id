package geolite

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/host/local"
	"github.com/stretchr/testify/require"

	testutils "github.com/pocket-id/pocket-id/backend/internal/utils/testing"
)

// testDownloadURL is the URL the mock HTTP client serves the database from
const testDownloadURL = "https://example.com/geolite/GeoLite2-City.tar.gz"

// testDatabasePath is the sample database published by MaxMind, see testdata/README.md
const testDatabasePath = "testdata/GeoLite2-City-Test.mmdb"

// readTestDatabase returns the raw sample GeoLite2 City database
func readTestDatabase(t *testing.T) []byte {
	t.Helper()

	data, err := os.ReadFile(testDatabasePath)
	require.NoError(t, err)

	return data
}

// newActorHostForTest starts a test actor host with the GeoLite actor registered on it
// The actor is registered as a regular actor rather than a singleton one, so the host doesn't bootstrap it and the test drives the actor explicitly
func newActorHostForTest(t *testing.T, opts actorOptions) *local.Host {
	t.Helper()

	return testutils.NewActorHostForTest(t, func(t *testing.T, h *local.Host) {
		err := h.RegisterActor(ActorType, NewActor(opts))
		require.NoError(t, err)
	})
}

// newActorServiceForTest is newActorHostForTest for tests that only need the actor service
func newActorServiceForTest(t *testing.T, opts actorOptions) *actor.Service {
	t.Helper()

	return newActorHostForTest(t, opts).Service()
}

// requireUpdateScheduledForTest asserts that the updater has armed a one-shot alarm due around the given time
func requireUpdateScheduledForTest(t *testing.T, host *local.Host, dueTime time.Time) {
	t.Helper()

	props, err := host.GetAlarm(t.Context(), ActorType, updaterActorID, alarmUpdate)
	require.NoError(t, err)
	require.Empty(t, props.Interval, "the update alarm must not repeat on a fixed interval")
	require.WithinDuration(t, dueTime, props.DueTime, time.Minute)
}

// storeDatabaseForTest writes a database directly into the reader's actor state, as the updater does after a download
func storeDatabaseForTest(t *testing.T, svc *actor.Service, data []byte, updatedAt time.Time) {
	t.Helper()

	err := svc.SetState(t.Context(), ActorType, actor.SingletonActorID, databaseState{
		Data:      data,
		UpdatedAt: updatedAt,
	}, nil)
	require.NoError(t, err)
}

// peekLookupForTest peeks the reader actor to resolve an IP address
func peekLookupForTest(t *testing.T, svc *actor.Service, ipAddress string) lookupResponse {
	t.Helper()

	response, err := lookupForTest(t.Context(), svc, false, ipAddress)
	require.NoError(t, err)

	return response
}

// invokeLookupForTest resolves an IP address through Invoke, which also loads the database when the reader has just activated
func invokeLookupForTest(t *testing.T, svc *actor.Service, ipAddress string) lookupResponse {
	t.Helper()

	response, err := lookupForTest(t.Context(), svc, true, ipAddress)
	require.NoError(t, err)

	return response
}

// lookupForTest performs a lookup without assertions, so it can be called from outside the test's goroutine, such as from the condition of require.Eventually
func lookupForTest(ctx context.Context, svc *actor.Service, exclusive bool, ipAddress string) (lookupResponse, error) {
	call := svc.Peek
	if exclusive {
		call = svc.Invoke
	}

	res, err := call(ctx, ActorType, actor.SingletonActorID, methodLookup, lookupRequest{IPAddress: ipAddress})
	if err != nil {
		return lookupResponse{}, err
	}
	if res == nil {
		return lookupResponse{}, errors.New("actor response was empty")
	}

	var response lookupResponse
	err = res.Decode(&response)
	if err != nil {
		return lookupResponse{}, err
	}

	return response, nil
}

// countingRoundTripper serves a fixed response for testDownloadURL and counts how many requests it has received
type countingRoundTripper struct {
	body       []byte
	statusCode int
	requests   atomic.Int32
}

func (rt *countingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.String() != testDownloadURL {
		return testutils.NewMockResponse(http.StatusNotFound, ""), nil
	}

	rt.requests.Add(1)

	statusCode := rt.statusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}

	return &http.Response{
		StatusCode:    statusCode,
		Body:          io.NopCloser(bytes.NewReader(rt.body)),
		Header:        make(http.Header),
		ContentLength: int64(len(rt.body)),
	}, nil
}

// newDownloadClientForTest returns an HTTP client that serves body at testDownloadURL, along with the transport that counts the requests it receives
func newDownloadClientForTest(body []byte) (*http.Client, *countingRoundTripper) {
	rt := &countingRoundTripper{body: body}
	return &http.Client{Transport: rt}, rt
}

// buildTarGzForTest returns a gzipped tarball holding the given files, mirroring the archive MaxMind publishes
func buildTarGzForTest(t *testing.T, files map[string][]byte) []byte {
	t.Helper()

	buf := &bytes.Buffer{}
	gzw := gzip.NewWriter(buf)
	tw := tar.NewWriter(gzw)

	for name, content := range files {
		err := tw.WriteHeader(&tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		})
		require.NoError(t, err)

		_, err = tw.Write(content)
		require.NoError(t, err)
	}

	require.NoError(t, tw.Close())
	require.NoError(t, gzw.Close())

	return buf.Bytes()
}
