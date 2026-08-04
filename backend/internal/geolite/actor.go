package geolite

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/italypaleale/francis/actor"
	"github.com/oschwald/maxminddb-golang/v2"

	"github.com/pocket-id/pocket-id/backend/internal/common"
)

// The GeoLite actor owns the GeoLite2 City database for the entire cluster.
// The database lives in the actor state store.
//
// The actor type is backed by two instances, each with its own turn lock:
//   - the reader, which is the cluster-wide singleton: it keeps the database open in memory and answers lookups through Peek, so lookups run concurrently
//   - the updater, which owns the alarm that refreshes the database - this is separate because downloading holds the updater's turn for its entire duration, which would block all readers
//
// The updater writes the refreshed database straight into the reader's state and then asks the reader to reload it, so the database itself is never sent between the two instances.

// ActorType is the actor type for the GeoLite actor
const ActorType = "GeoLite"

const (
	// updaterActorID is the fixed actor ID of the updater instance, distinct from the reader's singleton ID so the two have independent turn locks
	updaterActorID = "updater"

	// methodLookup resolves an IP address against the database
	// It is served through Peek, under the actor's shared lock, so lookups run concurrently, and through Invoke, which additionally loads the database when the reader has just activated
	methodLookup = "lookup"
	// methodReload makes the reader pick up the database the updater has just written to the state store
	methodReload = "reload"
	// methodUpdate runs the same refresh the alarm performs, on demand
	methodUpdate = "update"

	// alarmUpdate is the name of the updater's alarm
	// The alarm doesn't repeat on a fixed interval: every run schedules the next one for when the database it just looked at goes stale
	alarmUpdate = "update"
	// databaseMaxAge is how old the stored database is allowed to get before it's downloaded again, which is also what paces the update alarm
	databaseMaxAge = 14 * 24 * time.Hour
	// updateRetryInterval is how long the updater waits before trying again after a failed download
	updateRetryInterval = time.Hour
	// downloadTimeout bounds a single download of the database
	downloadTimeout = 10 * time.Minute
	// stateTimeout bounds the state store operations performed by the actor
	stateTimeout = 30 * time.Second
)

// databaseState is the actor state that holds the GeoLite2 City database
// It is stored under the reader's singleton actor ID, and the updater is its only writer
type databaseState struct {
	// Data is the raw MaxMind DB file
	Data []byte `json:"data"`
	// UpdatedAt is the time the database was downloaded
	UpdatedAt time.Time `json:"updatedAt"`
}

// updaterState is the actor state of the updater instance
// It only tracks when the database was last refreshed, so the alarm can decide whether a new download is due without reading the (much larger) database state
type updaterState struct {
	// UpdatedAt is the time the database was last downloaded
	UpdatedAt time.Time `json:"updatedAt"`
}

// lookupRequest is the request body of the lookup method
type lookupRequest struct {
	IPAddress string `json:"ipAddress"`
}

// lookupStatus reports whether the reader was able to perform the lookup
type lookupStatus string

const (
	// lookupStatusOK means the lookup was performed
	// Country and City are still empty when the address isn't in the database, or when no database has been downloaded yet
	lookupStatusOK lookupStatus = "ok"
	// lookupStatusNotLoaded means the reader has just activated and hasn't read the database from the state store yet
	// Loading it mutates the actor, which a Peek must not do, so the caller repeats the lookup through Invoke
	lookupStatusNotLoaded lookupStatus = "not_loaded"
)

// lookupResponse is the response of the lookup method
type lookupResponse struct {
	Status  lookupStatus `json:"status"`
	Country string       `json:"country"`
	City    string       `json:"city"`
}

// geoLiteRecord is the subset of a GeoLite2 City record that Pocket ID uses
type geoLiteRecord struct {
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Country struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
}

// actorOptions contains the configuration shared by both instances of the actor
type actorOptions struct {
	httpClient  *http.Client
	downloadURL string
	licenseKey  string
	// updaterDisabled stops the update alarm from being registered, for deployments that have no way to download the database
	updaterDisabled bool
}

// NewActor returns the factory that allocates the GeoLite actor
func NewActor(opts actorOptions) actor.Factory {
	return func(actorID string, service *actor.Service) actor.Actor {
		log := slog.With(
			slog.String("scope", "actor"),
			slog.String("actorType", ActorType),
			slog.String("actorID", actorID),
		)

		// Allocate the updater
		if actorID == updaterActorID {
			return &updaterActor{
				log:    log,
				opts:   opts,
				svc:    service,
				client: actor.NewActorClient[updaterState](ActorType, actorID, service),
			}
		}

		// Allocate the reader
		return &readerActor{
			log:             log,
			svc:             service,
			readerActorID:   actorID,
			updaterDisabled: opts.updaterDisabled,
		}
	}
}

// readerActor keeps the GeoLite2 City database open in memory and resolves IP addresses against it
type readerActor struct {
	log           *slog.Logger
	svc           *actor.Service
	readerActorID string

	updaterDisabled bool

	// The fields below hold the database in memory, and they need no lock of their own
	// They are only written from Bootstrap and Invoke, which the framework runs under the actor's exclusive turn lock, and that excludes every concurrent Peek for the duration
	// db is the database currently open in memory, or nil when the state store holds none
	db *maxminddb.Reader
	// dbUpdatedAt is the UpdatedAt of the database in memory
	dbUpdatedAt time.Time
	// loaded reports whether the state store has been read since this activation
	loaded bool
}

// Bootstrap implements actor.ActorBootstrapper
// The host drives it on every startup, routed to the single owning host, so it must stay idempotent
func (a *readerActor) Bootstrap(parentCtx context.Context, _ actor.Envelope) error {
	// Register (or remove) the alarm that refreshes the database
	// It's set on the updater instance so a download never blocks lookups on this one
	err := a.reconcileUpdateAlarm(parentCtx)
	if err != nil {
		return err
	}

	// Warm up the database so the first lookup doesn't have to pay for reading it
	// A failure here is not fatal: the lookup path loads the database on demand anyway
	err = a.load(parentCtx)
	if err != nil {
		a.log.WarnContext(parentCtx, "Failed to load the GeoLite2 City database at startup, will retry on the first lookup", slog.Any("error", err))
	}

	return nil
}

// reconcileUpdateAlarm registers the update alarm on the updater instance, or removes it when the updater is disabled
func (a *readerActor) reconcileUpdateAlarm(parentCtx context.Context) error {
	ctx, cancel := context.WithTimeout(parentCtx, stateTimeout)
	defer cancel()

	if a.updaterDisabled {
		// The updater may have been enabled in a previous run, so make sure a leftover alarm doesn't keep firing
		err := a.svc.DeleteAlarm(ctx, ActorType, updaterActorID, alarmUpdate)
		if err != nil && !errors.Is(err, actor.ErrAlarmNotFound) {
			return fmt.Errorf("error deleting the GeoLite update alarm: %w", err)
		}

		return nil
	}

	// The alarm is due right away so a database that is missing or stale is refreshed as soon as the cluster starts, and so an alarm that was lost (for example, deleted after repeated download failures) is restored
	// Bringing it forward on every startup costs one state read: the updater re-downloads the database only once it's older than databaseMaxAge, and either way it schedules the next run itself
	err := a.svc.SetAlarm(ctx, ActorType, updaterActorID, alarmUpdate, actor.AlarmProperties{
		DueTime: time.Now(),
	})
	if err != nil {
		return fmt.Errorf("error setting the GeoLite update alarm: %w", err)
	}

	return nil
}

// Peek implements actor.ActorPeek
// It runs under the actor's shared lock, so several lookups are served concurrently
func (a *readerActor) Peek(_ context.Context, method string, data actor.Envelope) (any, error) {
	if method != methodLookup {
		return nil, common.ErrUnsupportedActorMethod{Method: method}
	}

	request, err := decodeLookupRequest(data, method)
	if err != nil {
		return nil, err
	}

	// A Peek must not mutate the actor, so it can't read the database in: it tells the caller to come back through Invoke instead
	if !a.loaded {
		return lookupResponse{Status: lookupStatusNotLoaded}, nil
	}

	return a.lookup(request)
}

// Invoke implements actor.ActorInvoke
func (a *readerActor) Invoke(ctx context.Context, method string, data actor.Envelope) (any, error) {
	switch method {
	case methodReload:
		// The updater has written a new database, so read it back from the state store
		// A failed load leaves the database currently in memory in place, so a corrupted update doesn't take lookups down with it
		return nil, a.load(ctx)

	case methodLookup:
		// This is how a lookup gets served right after the reader activates, when Peek has nothing in memory to answer from
		if !a.loaded {
			err := a.load(ctx)
			if err != nil {
				return nil, err
			}
		}

		request, err := decodeLookupRequest(data, method)
		if err != nil {
			return nil, err
		}

		return a.lookup(request)

	default:
		return nil, common.ErrUnsupportedActorMethod{Method: method}
	}
}

// lookup resolves an IP address against the database in memory
// Country and City are empty when no database is available, or when the address isn't in it
func (a *readerActor) lookup(request lookupRequest) (lookupResponse, error) {
	addr, err := netip.ParseAddr(request.IPAddress)
	if err != nil {
		return lookupResponse{}, fmt.Errorf("failed to parse IP address: %w", err)
	}

	if a.db == nil {
		// No database has been downloaded yet
		return lookupResponse{Status: lookupStatusOK}, nil
	}

	result := a.db.Lookup(addr)
	if !result.Found() {
		return lookupResponse{Status: lookupStatusOK}, nil
	}

	var record geoLiteRecord
	err = result.Decode(&record)
	if err != nil {
		return lookupResponse{}, fmt.Errorf("failed to decode database record: %w", err)
	}

	return lookupResponse{
		Status:  lookupStatusOK,
		Country: record.Country.Names["en"],
		City:    record.City.Names["en"],
	}, nil
}

// load reads the database from the actor state store and opens it in memory
// Callers must be running under the actor's exclusive turn lock, since this writes the fields a lookup reads
// The state store is read through the actor service rather than through an actor client because the client caches the state it has read, and the updater writes the database from another instance
func (a *readerActor) load(parentCtx context.Context) error {
	ctx, cancel := context.WithTimeout(parentCtx, stateTimeout)
	defer cancel()

	var state databaseState
	err := a.svc.GetState(ctx, ActorType, a.readerActorID, &state)
	switch {
	case errors.Is(err, actor.ErrStateNotFound):
		// No database has been downloaded yet
		a.db = nil
		a.dbUpdatedAt = time.Time{}
		a.loaded = true
		return nil
	case err != nil:
		return fmt.Errorf("error retrieving actor state: %w", err)
	}

	// Nothing to do if the database in memory is already the one in the state store
	if a.db != nil && a.dbUpdatedAt.Equal(state.UpdatedAt) {
		return nil
	}

	// The database is opened from a byte slice rather than a memory-mapped file, so the one being replaced holds no resource to release: dropping the reference is enough
	db, err := maxminddb.OpenBytes(state.Data)
	if err != nil {
		return fmt.Errorf("error opening the GeoLite2 City database: %w", err)
	}

	a.db = db
	a.dbUpdatedAt = state.UpdatedAt
	a.loaded = true

	a.log.InfoContext(ctx, "Loaded the GeoLite2 City database",
		slog.Time("updatedAt", state.UpdatedAt),
		slog.Int("size", len(state.Data)),
	)

	return nil
}

// updaterActor downloads the GeoLite2 City database and stores it for the reader
type updaterActor struct {
	log    *slog.Logger
	opts   actorOptions
	svc    *actor.Service
	client actor.Client[updaterState]
}

// Alarm implements actor.ActorAlarm
func (a *updaterActor) Alarm(ctx context.Context, name string, _ actor.Envelope) error {
	if name != alarmUpdate {
		return fmt.Errorf("unsupported alarm '%s' for the %s actor", name, ActorType)
	}

	return a.update(ctx)
}

// Invoke implements actor.ActorInvoke
func (a *updaterActor) Invoke(ctx context.Context, method string, _ actor.Envelope) (any, error) {
	if method != methodUpdate {
		return nil, common.ErrUnsupportedActorMethod{Method: method}
	}

	return nil, a.update(ctx)
}

// update downloads the database and stores it for the reader, unless the stored one is still recent enough
// It always leaves an alarm scheduled for the next run, so the updater paces itself instead of waking up on a fixed interval
func (a *updaterActor) update(parentCtx context.Context) error {
	state, err := a.client.GetState(parentCtx)
	if err != nil {
		return fmt.Errorf("error retrieving actor state: %w", err)
	}

	// The database is still recent enough, so only re-arm the alarm for the moment it goes stale
	// This is the path a restart takes, since the reader brings the alarm forward when it bootstraps
	if nextUpdate := state.UpdatedAt.Add(databaseMaxAge); nextUpdate.After(time.Now()) {
		a.log.DebugContext(parentCtx, "GeoLite2 City database is up-to-date", slog.Time("updatedAt", state.UpdatedAt))
		return a.scheduleUpdate(parentCtx, nextUpdate)
	}

	a.log.InfoContext(parentCtx, "Updating the GeoLite2 City database")

	downloadCtx, cancel := context.WithTimeout(parentCtx, downloadTimeout)
	defer cancel()
	data, err := downloadDatabase(downloadCtx, a.opts.httpClient, a.opts.downloadURL, a.opts.licenseKey)
	if err != nil {
		// Schedule the retry here rather than returning the error, which would leave the framework to retry the alarm and then delete it once the attempts run out, stopping updates altogether
		a.log.ErrorContext(parentCtx, "Failed to update the GeoLite2 City database, will try again later",
			slog.Duration("retryIn", updateRetryInterval),
			slog.Any("error", err),
		)
		return a.scheduleUpdate(parentCtx, time.Now().Add(updateRetryInterval))
	}

	updatedAt := time.Now()

	// Store the database under the reader's actor ID, which is where the reader looks for it
	// It goes through the state store rather than through an invocation so the database is never sent over the wire between the two instances
	stateCtx, cancel := context.WithTimeout(parentCtx, stateTimeout)
	defer cancel()
	err = a.svc.SetState(stateCtx, ActorType, actor.SingletonActorID, databaseState{
		Data:      data,
		UpdatedAt: updatedAt,
	}, nil)
	if err != nil {
		return fmt.Errorf("error saving the database in the actor state store: %w", err)
	}

	// Record the download only after the database has been stored, so a failure in between causes a retry rather than leaving the cluster without a database until the next cycle
	stateCtx, cancel = context.WithTimeout(parentCtx, stateTimeout)
	defer cancel()
	err = a.client.SetState(stateCtx, updaterState{UpdatedAt: updatedAt}, nil)
	if err != nil {
		return fmt.Errorf("error saving actor state: %w", err)
	}

	a.log.InfoContext(parentCtx, "GeoLite2 City database successfully updated", slog.Int("size", len(data)))

	// Ask the reader to pick up the new database
	// A failure here isn't fatal: the reader loads the new database the next time it activates
	invokeCtx, cancel := context.WithTimeout(parentCtx, stateTimeout)
	defer cancel()
	_, err = a.svc.Invoke(invokeCtx, ActorType, actor.SingletonActorID, methodReload, nil)
	if err != nil {
		a.log.WarnContext(parentCtx, "Failed to reload the GeoLite2 City database in the reader actor", slog.Any("error", err))
	}

	return a.scheduleUpdate(parentCtx, updatedAt.Add(databaseMaxAge))
}

// scheduleUpdate arms the alarm that performs the next update
// It replaces the alarm that is currently executing, which the framework supports: the replacement invalidates the running occurrence's lease, so the framework leaves the new alarm alone instead of deleting the completed one
func (a *updaterActor) scheduleUpdate(parentCtx context.Context, dueTime time.Time) error {
	ctx, cancel := context.WithTimeout(parentCtx, stateTimeout)
	defer cancel()

	err := a.client.SetAlarm(ctx, alarmUpdate, actor.AlarmProperties{DueTime: dueTime})
	if err != nil {
		return fmt.Errorf("error scheduling the next GeoLite update: %w", err)
	}

	a.log.DebugContext(parentCtx, "Scheduled the next GeoLite2 City database update", slog.Time("dueTime", dueTime))

	return nil
}

// decodeLookupRequest decodes the request body of a lookup invocation
func decodeLookupRequest(data actor.Envelope, method string) (lookupRequest, error) {
	if data == nil {
		return lookupRequest{}, fmt.Errorf("request body is empty for method '%s'", method)
	}

	var request lookupRequest
	err := data.Decode(&request)
	if err != nil {
		return lookupRequest{}, fmt.Errorf("request body is not valid for method '%s': %w", method, err)
	}

	return request, nil
}
