// This file contains code adapted from https://github.com/dapr/kit/blob/e3d4a8f1b4ae54d36fb87892b25eaa1cd3e11e61/jwkscache/cache.go
// Copyright (C) 2023 The Dapr Authors
// License (Apache-2.0): https://github.com/dapr/kit/blob/e3d4a8f1b4ae54d36fb87892b25eaa1cd3e11e61/LICENSE

package utils

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	testJWKS1 = `{"keys":[{"kid":"mykey","alg":"RS256","kty":"RSA","use":"sig","e":"AQAB","n":"3I2mdIK4mRRu-ywMrYjUZzBxt0NlAVLrMhGlaJsby7PWTMiLpZVip4SBD9GwnCU0TGFD7k2-7tfs0y9U6WV7MwgCjc9m_DUUGbE-kKjEU7JYkLzYlndys-6xuhD4Jf1hu9AZVdfXftpWSy_NNg6fVwTH4nckOAbOSL1hXToOYWQcDDW95Rhw3U4z04PqssEpRKn5KGBuTahNNNiZcWns99pChpLTxgdm93LjMBI1KCGBpOaz7fcQJ9V3c6rSwMKyY3IPm1LwS6PIs7xb2ZJ0Eb8A6MtCkGhgNsodpkxhqKbqtxI-KqTuZy9g4jb8WKjJq9lB9q-HPHoQqIEDom6P8w"}]}`
	testJWKS2 = `{"keys":[{"kid":"mykey","alg":"RS256","kty":"RSA","use":"sig","e":"AQAB","n":"3I2mdIK4mRRu-ywMrYjUZzBxt0NlAVLrMhGlaJsby7PWTMiLpZVip4SBD9GwnCU0TGFD7k2-7tfs0y9U6WV7MwgCjc9m_DUUGbE-kKjEU7JYkLzYlndys-6xuhD4Jf1hu9AZVdfXftpWSy_NNg6fVwTH4nckOAbOSL1hXToOYWQcDDW95Rhw3U4z04PqssEpRKn5KGBuTahNNNiZcWns99pChpLTxgdm93LjMBI1KCGBpOaz7fcQJ9V3c6rSwMKyY3IPm1LwS6PIs7xb2ZJ0Eb8A6MtCkGhgNsodpkxhqKbqtxI-KqTuZy9g4jb8WKjJq9lB9q-HPHoQqIEDom6P8w"},{"alg":"RS256","kty":"RSA","use":"sig","n":"yeNlzlub94YgerT030codqEztjfU_S6X4DbDA_iVKkjAWtYfPHDzz_sPCT1Axz6isZdf3lHpq_gYX4Sz-cbe4rjmigxUxr-FgKHQy3HeCdK6hNq9ASQvMK9LBOpXDNn7mei6RZWom4wo3CMvvsY1w8tjtfLb-yQwJPltHxShZq5-ihC9irpLI9xEBTgG12q5lGIFPhTl_7inA1PFK97LuSLnTJzW0bj096v_TMDg7pOWm_zHtF53qbVsI0e3v5nmdKXdFf9BjIARRfVrbxVxiZHjU6zL6jY5QJdh1QCmENoejj_ytspMmGW7yMRxzUqgxcAqOBpVm0b-_mW3HoBdjQ","e":"AQAB","kid":"testkey"}]}`
)

func TestJWKSCache(t *testing.T) {
	t.Run("init with value", func(t *testing.T) {
		cache := NewJWKSCache(testJWKS1, nil)
		err := cache.initCache(context.Background())
		require.NoError(t, err)

		set := cache.KeySet()
		require.Equal(t, 1, set.Len())

		key, ok := set.LookupKeyID("mykey")
		require.True(t, ok)
		require.NotNil(t, key)
	})

	t.Run("init with base64-encoded value", func(t *testing.T) {
		cache := NewJWKSCache(base64.StdEncoding.EncodeToString([]byte(testJWKS1)), nil)
		err := cache.initCache(context.Background())
		require.NoError(t, err)

		set := cache.KeySet()
		require.Equal(t, 1, set.Len())

		key, ok := set.LookupKeyID("mykey")
		require.True(t, ok)
		require.NotNil(t, key)
	})

	t.Run("init with HTTP client", func(t *testing.T) {
		// Create a custom HTTP client with a RoundTripper that doesn't require starting a TCP listener
		client := &http.Client{
			Transport: roundTripFn(func(r *http.Request) *http.Response {
				if r.Method != http.MethodGet || r.URL.Path != "/jwks.json" {
					return &http.Response{
						StatusCode: http.StatusNotFound,
						Header:     make(http.Header),
					}
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"content-type": []string{"application/json"},
					},
					Body: io.NopCloser(strings.NewReader(testJWKS1)),
				}
			}),
		}

		cache := NewJWKSCache("http://localhost/jwks.json", nil)
		cache.SetHTTPClient(client)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err := cache.initCache(ctx)
		require.NoError(t, err)

		set := cache.KeySet()
		require.Equal(t, 1, set.Len())

		key, ok := set.LookupKeyID("mykey")
		require.True(t, ok)
		require.NotNil(t, key)
	})

	t.Run("start and wait for init", func(t *testing.T) {
		cache := NewJWKSCache(testJWKS1, nil)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Start in background
		errCh := make(chan error)
		go func() {
			errCh <- cache.Start(ctx)
		}()

		// Wait for initialization
		err := cache.WaitForCacheReady(ctx)
		require.NoError(t, err)

		// Canceling the context should make Start() return
		cancel()
		require.NoError(t, <-errCh)
	})

	t.Run("start and init fails", func(t *testing.T) {
		// Create a custom HTTP client with a RoundTripper that doesn't require starting a TCP listener
		client := &http.Client{
			Transport: roundTripFn(func(r *http.Request) *http.Response {
				// Return an error
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
				}
			}),
		}

		cache := NewJWKSCache("https://localhost/jwks.json", nil)
		cache.SetHTTPClient(client)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Start in background
		errCh := make(chan error)
		go func() {
			errCh <- cache.Start(ctx)
		}()

		// Wait for initialization
		err := cache.WaitForCacheReady(ctx)
		require.Error(t, err)
		require.ErrorIs(t, err, context.DeadlineExceeded)

		// Canceling the context should make Start() return with the init error
		cancel()
		require.ErrorIs(t, <-errCh, err)
	})

	t.Run("start and init times out", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()

		// Create a custom HTTP client with a RoundTripper that doesn't require starting a TCP listener
		client := &http.Client{
			Transport: roundTripFn(func(r *http.Request) *http.Response {
				// Wait until context is canceled
				<-ctx.Done()

				// Sleep for another 500ms
				time.Sleep(500 * time.Millisecond)

				// Return an error
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
				}
			}),
		}

		cache := NewJWKSCache("https://localhost/jwks.json", nil)
		cache.SetHTTPClient(client)

		// Start in background
		errCh := make(chan error)
		go func() {
			errCh <- cache.Start(ctx)
		}()

		// Wait for initialization
		err := cache.WaitForCacheReady(context.Background())
		require.Error(t, err)
		require.ErrorIs(t, err, context.DeadlineExceeded)

		// Canceling the context should make Start() return with the init error
		cancel()
		require.Equal(t, err, <-errCh)
	})
}

type roundTripFn func(req *http.Request) *http.Response

func (f roundTripFn) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req), nil
}
