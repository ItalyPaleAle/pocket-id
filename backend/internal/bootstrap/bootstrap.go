package bootstrap

import (
	"context"

	_ "github.com/golang-migrate/migrate/v4/source/file"

	"github.com/pocket-id/pocket-id/backend/internal/service"
)

func Bootstrap() {
	initApplicationImages()

	migrateConfigDBConnstring()

	db := newDatabase()
	appConfigService := service.NewAppConfigService(db)

	profilePictureService := service.NewProfilePictureService()
	profilePictureService.Run(context.TODO())

	migrateKey()

	migrateProfilePictures(db, profilePictureService)

	initRouter(db, appConfigService, profilePictureService)
}
