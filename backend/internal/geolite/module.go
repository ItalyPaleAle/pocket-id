package geolite

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/italypaleale/francis/host/local"

	"github.com/pocket-id/pocket-id/backend/internal/common"
)

type Dependencies struct {
	Actors     *local.Host
	HTTPClient *http.Client

	// DownloadURL is the URL the GeoLite2 City database is downloaded from
	// When it contains a "%s" placeholder, it is replaced with LicenseKey
	DownloadURL string
	// LicenseKey is the MaxMind license key
	LicenseKey string
}

type Module struct {
	service *Service
}

func New(deps Dependencies) (*Module, error) {
	opts := actorOptions{
		httpClient:  deps.HTTPClient,
		downloadURL: deps.DownloadURL,
		licenseKey:  deps.LicenseKey,
	}

	if deps.LicenseKey == "" && deps.DownloadURL == common.MaxMindGeoLiteCityUrl {
		// Warn the user, and disable the periodic updater
		slog.Warn("MAXMIND_LICENSE_KEY environment variable is empty: the GeoLite2 City database won't be updated")
		opts.updaterDisabled = true
	}

	// The reader is a singleton actor, so the host bootstraps it at startup and every replica reaches the same instance
	// Its idle timeout is disabled to keep the database in memory, rather than reading it back from the state store after every idle period
	err := deps.Actors.RegisterSingletonActor(ActorType, NewActor(opts), local.WithIdleTimeout(-1))
	if err != nil {
		return nil, fmt.Errorf("error registering the %s actor: %w", ActorType, err)
	}

	return &Module{
		service: newService(deps.Actors.Service()),
	}, nil
}

// GetLocationByIP returns the country and city of the given IP address
func (m *Module) GetLocationByIP(ctx context.Context, ipAddress string) (country string, city string, err error) {
	return m.service.GetLocationByIP(ctx, ipAddress)
}
