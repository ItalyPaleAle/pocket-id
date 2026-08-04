package geolite

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/oschwald/maxminddb-golang/v2"
)

const (
	// databaseFileName is the name of the database inside the archive published by MaxMind
	databaseFileName = "GeoLite2-City.mmdb"
	// maxDatabaseSize is the largest (decompressed) database we accept
	maxDatabaseSize = 300 << 20 // 300 MB
)

// downloadDatabase downloads the GeoLite2 City database and returns the raw MaxMind DB file
// When downloadURL contains a "%s" placeholder, it is replaced with the license key
func downloadDatabase(ctx context.Context, httpClient *http.Client, downloadURL string, licenseKey string) ([]byte, error) {
	if strings.Contains(downloadURL, "%s") {
		downloadURL = fmt.Sprintf(downloadURL, licenseKey)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	res, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download database: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download database, received HTTP %d", res.StatusCode)
	}

	data, err := extractDatabase(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to extract database: %w", err)
	}

	// Make sure the database isn't corrupted before it's handed over to the caller, which stores it for the whole cluster
	db, err := maxminddb.OpenBytes(data)
	if err != nil {
		return nil, fmt.Errorf("failed to open downloaded database: %w", err)
	}
	_ = db.Close()

	return data, nil
}

// extractDatabase returns the raw MaxMind DB file contained in the downloaded body
// The body is either the gzipped tarball published by MaxMind, or the database file itself
func extractDatabase(reader io.Reader) ([]byte, error) {
	// Read the first two bytes to check for the gzip magic number
	magic := make([]byte, 2)
	_, err := io.ReadFull(reader, magic)
	if err != nil {
		return nil, fmt.Errorf("failed to read magic number: %w", err)
	}
	reader = io.MultiReader(bytes.NewReader(magic), reader)

	// If the body doesn't start with the gzip magic number, assume it's a plain database file
	// Gosec returns false positive for "G602: slice index out of range"
	//nolint:gosec
	if magic[0] != 0x1f || magic[1] != 0x8b {
		return readDatabase(reader)
	}

	gzr, err := gzip.NewReader(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close()

	tarReader := tar.NewReader(gzr)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("failed to read tar archive: %w", err)
		}

		// The archive contains the database in a versioned folder, alongside other files such as the license
		if header.Typeflag != tar.TypeReg || path.Base(header.Name) != databaseFileName {
			continue
		}

		return readDatabase(tarReader)
	}

	return nil, errors.New(databaseFileName + " not found in archive")
}

// readDatabase reads the database in full, refusing anything larger than maxDatabaseSize
func readDatabase(reader io.Reader) ([]byte, error) {
	return readDatabaseWithLimit(reader, maxDatabaseSize)
}

// readDatabaseWithLimit is readDatabase with an explicit limit, which lets tests exercise the limit without reading hundreds of megabytes
func readDatabaseWithLimit(reader io.Reader, limit int64) ([]byte, error) {
	// Read one byte more than the limit, so content that is exactly at the limit can be told apart from content that exceeds it
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read database: %w", err)
	}

	if int64(len(data)) > limit {
		return nil, errors.New("database size exceeds maximum allowed limit")
	}

	return data, nil
}
