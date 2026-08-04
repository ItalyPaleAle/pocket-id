package geolite

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractDatabase(t *testing.T) {
	database := readTestDatabase(t)

	t.Run("gzipped tarball", func(t *testing.T) {
		archive := buildTarGzForTest(t, map[string][]byte{
			"GeoLite2-City_20260101/COPYRIGHT.txt":       []byte("copyright"),
			"GeoLite2-City_20260101/LICENSE.txt":         []byte("license"),
			"GeoLite2-City_20260101/" + databaseFileName: database,
		})

		data, err := extractDatabase(bytes.NewReader(archive))
		require.NoError(t, err)
		require.Equal(t, database, data)
	})

	t.Run("plain database file", func(t *testing.T) {
		// A custom GEOLITE_DB_URL may serve the database uncompressed
		data, err := extractDatabase(bytes.NewReader(database))
		require.NoError(t, err)
		require.Equal(t, database, data)
	})

	t.Run("tarball without the database", func(t *testing.T) {
		archive := buildTarGzForTest(t, map[string][]byte{
			"GeoLite2-City_20260101/COPYRIGHT.txt": []byte("copyright"),
		})

		_, err := extractDatabase(bytes.NewReader(archive))
		require.Error(t, err)
		require.ErrorContains(t, err, "not found in archive")
	})

	t.Run("truncated gzip stream", func(t *testing.T) {
		archive := buildTarGzForTest(t, map[string][]byte{"GeoLite2-City_20260101/" + databaseFileName: database})

		_, err := extractDatabase(bytes.NewReader(archive[:len(archive)/2]))
		require.Error(t, err)
	})

	t.Run("empty body", func(t *testing.T) {
		_, err := extractDatabase(strings.NewReader(""))
		require.Error(t, err)
		require.ErrorContains(t, err, "failed to read magic number")
	})
}

func TestReadDatabase(t *testing.T) {
	t.Run("exactly at the limit", func(t *testing.T) {
		data, err := readDatabaseWithLimit(bytes.NewReader(bytes.Repeat([]byte{0x01}, 1024)), 1024)
		require.NoError(t, err)
		require.Len(t, data, 1024)
	})

	t.Run("over the limit", func(t *testing.T) {
		_, err := readDatabaseWithLimit(endlessReader{}, 1024)
		require.Error(t, err)
		require.ErrorContains(t, err, "exceeds maximum allowed limit")
	})
}

func TestDownloadDatabase(t *testing.T) {
	database := readTestDatabase(t)
	archive := buildTarGzForTest(t, map[string][]byte{"GeoLite2-City_20260101/" + databaseFileName: database})

	t.Run("success", func(t *testing.T) {
		httpClient, transport := newDownloadClientForTest(archive)

		data, err := downloadDatabase(t.Context(), httpClient, testDownloadURL, "")
		require.NoError(t, err)
		require.Equal(t, database, data)
		require.Equal(t, int32(1), transport.requests.Load())
	})

	t.Run("license key placeholder", func(t *testing.T) {
		// The default MaxMind URL carries the license key, which is filled in at download time
		var requestedURL string
		httpClient := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			requestedURL = req.URL.String()
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(archive)),
				Header:     make(http.Header),
			}, nil
		})}

		_, err := downloadDatabase(t.Context(), httpClient, "https://example.com/download?license_key=%s", "secret-key")
		require.NoError(t, err)
		require.Equal(t, "https://example.com/download?license_key=secret-key", requestedURL)
	})

	t.Run("non-200 response", func(t *testing.T) {
		httpClient, transport := newDownloadClientForTest(nil)
		transport.statusCode = http.StatusUnauthorized

		_, err := downloadDatabase(t.Context(), httpClient, testDownloadURL, "")
		require.Error(t, err)
		require.ErrorContains(t, err, "received HTTP 401")
	})

	t.Run("corrupted database", func(t *testing.T) {
		corrupted := buildTarGzForTest(t, map[string][]byte{
			"GeoLite2-City_20260101/" + databaseFileName: []byte("not a database"),
		})
		httpClient, _ := newDownloadClientForTest(corrupted)

		_, err := downloadDatabase(t.Context(), httpClient, testDownloadURL, "")
		require.Error(t, err)
		require.ErrorContains(t, err, "failed to open downloaded database")
	})
}

// endlessReader returns an unbounded stream of zeroes, to exercise the download size limit
type endlessReader struct{}

func (endlessReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
