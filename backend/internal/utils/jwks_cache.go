// This file contains code adapted from https://github.com/dapr/kit/blob/e3d4a8f1b4ae54d36fb87892b25eaa1cd3e11e61/jwkscache/cache.go
// Copyright (C) 2023 The Dapr Authors
// License (Apache-2.0): https://github.com/dapr/kit/blob/e3d4a8f1b4ae54d36fb87892b25eaa1cd3e11e61/LICENSE

package utils

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	httprc "github.com/lestrrat-go/httprc/v3"
	"github.com/lestrrat-go/httprc/v3/errsink"
	"github.com/lestrrat-go/jwx/v3/jwk"
)

const (
	// Timeout for network requests.
	defaultRequestTimeout = 30 * time.Second
	// Minimum interval for refreshing a JWKS from a URL if a key is not found in the cache.
	defaultMinRefreshInterval = 10 * time.Minute
)

var JWKSCacheStartedErr = errors.New("cache is already running")

// JWKSCache is a cache of JWKS objects.
// It fetches a JWKS object from a file on disk, a URL, or from a value passed as-is.
type JWKSCache struct {
	location           string
	requestTimeout     time.Duration
	minRefreshInterval time.Duration

	jwks    jwk.Set
	logger  *slog.Logger
	lock    sync.RWMutex
	client  *http.Client
	running atomic.Bool
	initCh  chan error
}

// NewJWKSCache creates a new JWKSCache object.
func NewJWKSCache(location string, logger *slog.Logger) *JWKSCache {
	if logger == nil {
		logger = slog.Default()
	}

	return &JWKSCache{
		location: location,
		logger:   logger,

		requestTimeout:     defaultRequestTimeout,
		minRefreshInterval: defaultMinRefreshInterval,

		initCh: make(chan error, 1),
	}
}

// Start the JWKS cache.
// This method blocks until the context is canceled.
func (c *JWKSCache) Start(ctx context.Context) error {
	if !c.running.CompareAndSwap(false, true) {
		return JWKSCacheStartedErr
	}
	defer c.running.Store(false)

	// Init the cache
	err := c.initCache(ctx)
	if err != nil {
		err = fmt.Errorf("failed to init cache: %w", err)
		// Store the error in the initCh, then close it
		c.initCh <- err
		close(c.initCh)
		return err
	}

	// Close initCh
	close(c.initCh)

	// Block until context is canceled
	<-ctx.Done()

	return nil
}

// SetRequestTimeout sets the timeout for network requests.
func (c *JWKSCache) SetRequestTimeout(requestTimeout time.Duration) {
	c.requestTimeout = requestTimeout
}

// SetMinRefreshInterval sets the minimum interval for refreshing a JWKS from a URL if a key is not found in the cache.
func (c *JWKSCache) SetMinRefreshInterval(minRefreshInterval time.Duration) {
	c.minRefreshInterval = minRefreshInterval
}

// SetHTTPClient sets the HTTP client object to use.
func (c *JWKSCache) SetHTTPClient(client *http.Client) {
	c.client = client
}

// KeySet returns the jwk.Set with the current keys.
func (c *JWKSCache) KeySet() jwk.Set {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.jwks
}

// WaitForCacheReady pauses until the cache is ready (the initial JWKS has been fetched) or the passed ctx is canceled.
// It will return the initialization error.
func (c *JWKSCache) WaitForCacheReady(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-c.initCh:
		return err
	}
}

// Init the cache from the given location.
func (c *JWKSCache) initCache(ctx context.Context) error {
	if len(c.location) == 0 {
		return errors.New("property 'location' must not be empty")
	}

	// If the location starts with "https://" or "http://", treat it as URL
	if strings.HasPrefix(c.location, "https://") {
		return c.initJWKSFromURL(ctx, c.location)
	} else if strings.HasPrefix(c.location, "http://") {
		c.logger.Warn("Loading JWK from an HTTP endpoint without TLS: this is not recommended on production environments.")
		return c.initJWKSFromURL(ctx, c.location)
	}

	// Treat the location as the actual JWKS
	// First, check if it's base64-encoded (remove trailing padding chars if present first)
	locationJSON, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(c.location, "="))
	if err != nil {
		// Assume it's already JSON, not encoded
		locationJSON = []byte(c.location)
	}

	// Try decoding from JSON
	c.jwks, err = jwk.Parse(locationJSON)
	if err != nil {
		return errors.New("failed to parse property 'location': not a URL or JSON value (optionally base64-encoded)")
	}

	return nil
}

func (c *JWKSCache) initJWKSFromURL(ctx context.Context, url string) error {
	// We need to create a custom HTTP client (if we don't have one already) because otherwise there's no timeout.
	if c.client == nil {
		c.client = &http.Client{
			Timeout: c.requestTimeout,
		}

		defaultTransport, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			// Indicates a development-time error
			panic("Default transport is not of type *http.Transport")
		}
		transport := defaultTransport.Clone()
		transport.TLSClientConfig.MinVersion = tls.VersionTLS12
		c.client.Transport = transport
	}

	// Create the JWKS cache
	cache, err := jwk.NewCache(ctx,
		httprc.NewClient(
			httprc.WithErrorSink(errsink.NewSlog(c.logger)),
			httprc.WithHTTPClient(c.client),
		),
	)

	// Register the cache
	err = cache.Register(ctx, url,
		jwk.WithMinInterval(c.minRefreshInterval),
		jwk.WithHTTPClient(c.client),
	)
	if err != nil {
		return fmt.Errorf("failed to register JWKS cache: %w", err)
	}

	// Fetch the JWKS right away to start, so we can check it's valid and populate the cache
	refreshCtx, refreshCancel := context.WithTimeout(ctx, c.requestTimeout)
	_, err = cache.Refresh(refreshCtx, url)
	refreshCancel()
	if err != nil {
		return fmt.Errorf("failed to fetch JWKS: %w", err)
	}

	c.jwks, err = cache.CachedSet(url)
	if err != nil {
		return fmt.Errorf("failed to create cached set: %w", err)
	}

	return nil
}
