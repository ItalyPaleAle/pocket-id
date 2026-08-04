package geolite

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/italypaleale/francis/actor"

	"github.com/pocket-id/pocket-id/backend/internal/utils"
)

const (
	// internalNetworkCountry is reported for addresses that aren't routable on the public Internet
	internalNetworkCountry = "Internal Network"

	// lookupTimeout bounds a single lookup against the GeoLite actor
	lookupTimeout = 10 * time.Second
)

// Service resolves IP addresses to locations, through the GeoLite actor
type Service struct {
	actors *actor.Service
}

func newService(actors *actor.Service) *Service {
	return &Service{
		actors: actors,
	}
}

// GetLocationByIP returns the country and city of the given IP address
// Both are empty when the address isn't in the database, or when no database has been downloaded yet
func (s *Service) GetLocationByIP(parentCtx context.Context, ipAddress string) (country string, city string, err error) {
	if ipAddress == "" {
		return "", "", nil
	}

	// Check the IP address against known private IP ranges, which can be short-circuited
	ip := net.ParseIP(ipAddress)
	if ip != nil {
		switch {
		case utils.IsLocalIPv6(ip):
			return internalNetworkCountry, "LAN", nil
		case utils.IsTailscaleIP(ip):
			return internalNetworkCountry, "Tailscale", nil
		case utils.IsPrivateIP(ip):
			return internalNetworkCountry, "LAN", nil
		case utils.IsLocalhostIP(ip):
			return internalNetworkCountry, "localhost", nil
		}
	}

	addr, err := netip.ParseAddr(ipAddress)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse IP address: %w", err)
	}

	ctx, cancel := context.WithTimeout(parentCtx, lookupTimeout)
	defer cancel()

	request := lookupRequest{
		IPAddress: addr.String(),
	}

	// Lookups are peeked so they run concurrently with each other on the actor
	response, err := s.lookup(ctx, false, request)
	if err != nil {
		return "", "", err
	}

	// The reader has just activated and has no database in memory to answer from
	// Reading it in mutates the actor, which a Peek must not do, so the lookup is repeated through Invoke, which takes the actor's exclusive turn
	// This happens at most once per activation of the reader
	if response.Status == lookupStatusNotLoaded {
		response, err = s.lookup(ctx, true, request)
		if err != nil {
			return "", "", err
		}
	}

	return response.Country, response.City, nil
}

// lookup performs one lookup call against the reader actor, either as a Peek or, when exclusive is set, as an Invoke
func (s *Service) lookup(ctx context.Context, exclusive bool, request lookupRequest) (lookupResponse, error) {
	call := s.actors.Peek
	if exclusive {
		call = s.actors.Invoke
	}

	res, err := call(ctx, ActorType, actor.SingletonActorID, methodLookup, request)
	if err != nil {
		return lookupResponse{}, fmt.Errorf("error looking up the IP address in the %s actor: %w", ActorType, err)
	}
	if res == nil {
		return lookupResponse{}, errors.New(ActorType + " actor response was empty")
	}

	var response lookupResponse
	err = res.Decode(&response)
	if err != nil {
		return lookupResponse{}, fmt.Errorf("error decoding the %s actor response: %w", ActorType, err)
	}

	return response, nil
}
