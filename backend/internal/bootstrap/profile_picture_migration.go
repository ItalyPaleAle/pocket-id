package bootstrap

import (
	"fmt"
	"log"
	"os"

	"gorm.io/gorm"

	"github.com/pocket-id/pocket-id/backend/internal/common"
	"github.com/pocket-id/pocket-id/backend/internal/model"
	"github.com/pocket-id/pocket-id/backend/internal/service"
)

func migrateProfilePictures(db *gorm.DB, profilePictureService *service.ProfilePictureService) {
	err := migrateProfilePicturesInternal(db, profilePictureService)
	if err != nil {
		log.Fatalf("failed to perform migration of profile pictures: %v", err)
	}
}

// MigrateDefaultProfilePictures generates initials-based default profile pictures
// for users who don't have custom profile pictures
func migrateProfilePicturesInternal(db *gorm.DB, profilePictureService *service.ProfilePictureService) error {
	uploadPath := common.EnvConfig.UploadPath
	var users []model.User
	err := db.Find(&users).Error
	if err != nil {
		return fmt.Errorf("failed to fetch users: %w", err)
	}

	// Create defaults directory if it doesn't exist
	defaultsDir := uploadPath + "/profile-pictures/defaults"
	err = os.MkdirAll(defaultsDir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("failed to create defaults directory: %w", err)
	}

	for _, user := range users {
		// Add the request
		err := profilePictureService.EnsureDefaultProfilePicture(&user)
		if err != nil {
			return err
		}
	}

	return nil
}
