package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/pocket-id/pocket-id/backend/internal/common"
	"github.com/pocket-id/pocket-id/backend/internal/model"
	"github.com/pocket-id/pocket-id/backend/internal/utils"
	profilepicture "github.com/pocket-id/pocket-id/backend/internal/utils/image"
)

type ProfilePictureService struct {
	running   atomic.Bool
	queueLock sync.Mutex
	queue     []string
	queueMap  map[string]struct{}
	requestCh chan string
}

func NewProfilePictureService() *ProfilePictureService {
	return &ProfilePictureService{
		// Make this a buffered channel to ensure requests don't block
		requestCh: make(chan string, 2),
		queue:     []string{},
		queueMap:  map[string]struct{}{},
	}
}

func (s *ProfilePictureService) Run(ctx context.Context) error {
	if !s.running.CompareAndSwap(false, true) {
		return errors.New("service already running")
	}

	// Start the worker in background
	go func() {
		defer s.running.Store(false)

		// We can process in parallel up to the limit of (logical) CPUs
		jobCh := make(chan struct{}, runtime.NumCPU())

		for {
			select {
			case <-ctx.Done():
				// Stopping the worker
				return

			case req := <-s.requestCh:
				// Add the request to the queue
				// Note that this uses a map, so if the request for the same string is already present, this doesn't create a duplicate request
				s.queueLock.Lock()
				_, added := s.queueMap[req]
				if !added {
					s.queue = append(s.queue, req)
					s.queueMap[req] = struct{}{}
				}
				s.queueLock.Unlock()

				// Try to add to jobCh if it's not full already
				select {
				case jobCh <- struct{}{}:
					// Added
				default:
					// jobCh is full, so it will be picked up later
				}

			case <-jobCh:
				// Start a worker that processes jobs from the queue
				go func() {
					// Process until the queue is empty (or context is canceled)
					for {
						// Stop if context is canceled
						select {
						case <-ctx.Done():
							return
						default:
							// Continue
						}

						s.queueLock.Lock()
						if len(s.queue) == 0 {
							// Nothing else to process
							s.queueLock.Unlock()
							return
						}

						// First, we grab from the slice which makes sure no one else will process the same image
						// We remove from the map only after having done processing the request
						// This avoids that the same request is added twice
						req := s.queue[0]
						s.queue = s.queue[1:]
						s.queueLock.Unlock()

						// Process the request
						err := ensureProfilePictureJob(req)
						if err != nil {
							// Log errors but do nothing else, since we're in a background worker
							log.Printf("Failed to process default profile picture for initials '%s': %v", req, err)
						}

						// Now that we're done processing, remove from the map too
						s.queueLock.Lock()
						delete(s.queueMap, req)
						s.queueLock.Unlock()
					}
				}()
			}
		}
	}()

	return nil
}

// EnsureDefaultProfilePicture adds the request for the profile picture for the user.
// All the processing is done in background.
func (s *ProfilePictureService) EnsureDefaultProfilePicture(user *model.User) error {
	if !s.running.Load() {
		return errors.New("service is not running")
	}

	// Add the request
	s.requestCh <- user.Initials()

	return nil
}

func ensureProfilePictureJob(initials string) error {
	// Check if we have a cached default picture for these initials
	defaultPicturePath := defaultPicturePathForInitials(initials)
	exists, err := utils.FileExists(defaultPicturePath)
	if err != nil {
		return fmt.Errorf("failed to check if default profile picture exists: %w", err)
	}

	// If the picture already exists, do nothing
	if exists {
		return nil
	}

	// If no cached default picture exists, create one and save it for future use
	defaultPicture, err := profilepicture.CreateDefaultProfilePicture(initials)
	if err != nil {
		return fmt.Errorf("failed to create default profile picture: %w", err)
	}

	// Save the default picture for future use (in a goroutine to avoid blocking)
	defaultPictureCopy := bytes.NewReader(defaultPicture.Bytes())
	err = utils.SaveFileStream(defaultPictureCopy, defaultPicturePath)
	if err != nil {
		return fmt.Errorf("failed to save default profile picture: %w", err)
	}

	return nil
}

func defaultPicturePathForInitials(initials string) string {
	return common.EnvConfig.UploadPath + "/profile-pictures/defaults/" + initials + ".png"
}
