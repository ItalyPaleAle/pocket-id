package service

import (
	"context"
	"errors"
	"log"
	"mime/multipart"
	"os"
	"reflect"

	"github.com/pocket-id/pocket-id/backend/internal/common"
	"github.com/pocket-id/pocket-id/backend/internal/dto"
	"github.com/pocket-id/pocket-id/backend/internal/model"
	"github.com/pocket-id/pocket-id/backend/internal/utils"
	"gorm.io/gorm"
)

type AppConfigService struct {
	DbConfig *model.AppConfig
	db       *gorm.DB
}

func NewAppConfigService(ctx context.Context, db *gorm.DB) *AppConfigService {
	service := &AppConfigService{
		DbConfig: &defaultDbConfig,
		db:       db,
	}

	err := service.InitDbConfig(ctx)
	if err != nil {
		log.Fatalf("Failed to initialize app config service: %v", err)
	}

	return service
}

var defaultDbConfig = model.AppConfig{
	// General
	AppName: model.AppConfigVariable{
		Key:          "appName",
		Type:         "string",
		IsPublic:     true,
		DefaultValue: "Pocket ID",
	},
	SessionDuration: model.AppConfigVariable{
		Key:          "sessionDuration",
		Type:         "number",
		DefaultValue: "60",
	},
	EmailsVerified: model.AppConfigVariable{
		Key:          "emailsVerified",
		Type:         "bool",
		DefaultValue: "false",
	},
	AllowOwnAccountEdit: model.AppConfigVariable{
		Key:          "allowOwnAccountEdit",
		Type:         "bool",
		IsPublic:     true,
		DefaultValue: "true",
	},
	// Internal
	BackgroundImageType: model.AppConfigVariable{
		Key:          "backgroundImageType",
		Type:         "string",
		IsInternal:   true,
		DefaultValue: "jpg",
	},
	LogoLightImageType: model.AppConfigVariable{
		Key:          "logoLightImageType",
		Type:         "string",
		IsInternal:   true,
		DefaultValue: "svg",
	},
	LogoDarkImageType: model.AppConfigVariable{
		Key:          "logoDarkImageType",
		Type:         "string",
		IsInternal:   true,
		DefaultValue: "svg",
	},
	// Email
	SmtpHost: model.AppConfigVariable{
		Key:  "smtpHost",
		Type: "string",
	},
	SmtpPort: model.AppConfigVariable{
		Key:  "smtpPort",
		Type: "number",
	},
	SmtpFrom: model.AppConfigVariable{
		Key:  "smtpFrom",
		Type: "string",
	},
	SmtpUser: model.AppConfigVariable{
		Key:  "smtpUser",
		Type: "string",
	},
	SmtpPassword: model.AppConfigVariable{
		Key:  "smtpPassword",
		Type: "string",
	},
	SmtpTls: model.AppConfigVariable{
		Key:          "smtpTls",
		Type:         "string",
		DefaultValue: "none",
	},
	SmtpSkipCertVerify: model.AppConfigVariable{
		Key:          "smtpSkipCertVerify",
		Type:         "bool",
		DefaultValue: "false",
	},
	EmailLoginNotificationEnabled: model.AppConfigVariable{
		Key:          "emailLoginNotificationEnabled",
		Type:         "bool",
		DefaultValue: "false",
	},
	EmailOneTimeAccessEnabled: model.AppConfigVariable{
		Key:          "emailOneTimeAccessEnabled",
		Type:         "bool",
		IsPublic:     true,
		DefaultValue: "false",
	},
	// LDAP
	LdapEnabled: model.AppConfigVariable{
		Key:          "ldapEnabled",
		Type:         "bool",
		IsPublic:     true,
		DefaultValue: "false",
	},
	LdapUrl: model.AppConfigVariable{
		Key:  "ldapUrl",
		Type: "string",
	},
	LdapBindDn: model.AppConfigVariable{
		Key:  "ldapBindDn",
		Type: "string",
	},
	LdapBindPassword: model.AppConfigVariable{
		Key:  "ldapBindPassword",
		Type: "string",
	},
	LdapBase: model.AppConfigVariable{
		Key:  "ldapBase",
		Type: "string",
	},
	LdapUserSearchFilter: model.AppConfigVariable{
		Key:          "ldapUserSearchFilter",
		Type:         "string",
		DefaultValue: "(objectClass=person)",
	},
	LdapUserGroupSearchFilter: model.AppConfigVariable{
		Key:          "ldapUserGroupSearchFilter",
		Type:         "string",
		DefaultValue: "(objectClass=groupOfNames)",
	},
	LdapSkipCertVerify: model.AppConfigVariable{
		Key:          "ldapSkipCertVerify",
		Type:         "bool",
		DefaultValue: "false",
	},
	LdapAttributeUserUniqueIdentifier: model.AppConfigVariable{
		Key:  "ldapAttributeUserUniqueIdentifier",
		Type: "string",
	},
	LdapAttributeUserUsername: model.AppConfigVariable{
		Key:  "ldapAttributeUserUsername",
		Type: "string",
	},
	LdapAttributeUserEmail: model.AppConfigVariable{
		Key:  "ldapAttributeUserEmail",
		Type: "string",
	},
	LdapAttributeUserFirstName: model.AppConfigVariable{
		Key:  "ldapAttributeUserFirstName",
		Type: "string",
	},
	LdapAttributeUserLastName: model.AppConfigVariable{
		Key:  "ldapAttributeUserLastName",
		Type: "string",
	},
	LdapAttributeUserProfilePicture: model.AppConfigVariable{
		Key:  "ldapAttributeUserProfilePicture",
		Type: "string",
	},
	LdapAttributeGroupMember: model.AppConfigVariable{
		Key:          "ldapAttributeGroupMember",
		Type:         "string",
		DefaultValue: "member",
	},
	LdapAttributeGroupUniqueIdentifier: model.AppConfigVariable{
		Key:  "ldapAttributeGroupUniqueIdentifier",
		Type: "string",
	},
	LdapAttributeGroupName: model.AppConfigVariable{
		Key:  "ldapAttributeGroupName",
		Type: "string",
	},
	LdapAttributeAdminGroup: model.AppConfigVariable{
		Key:  "ldapAttributeAdminGroup",
		Type: "string",
	},
}

func (s *AppConfigService) UpdateAppConfig(ctx context.Context, input dto.AppConfigUpdateDto) ([]model.AppConfigVariable, error) {
	if common.EnvConfig.UiConfigDisabled {
		return nil, &common.UiConfigDisabledError{}
	}

	tx := s.db.Begin()
	defer func() {
		tx.Rollback()
	}()

	var err error

	rt := reflect.ValueOf(input).Type()
	rv := reflect.ValueOf(input)

	savedConfigVariables := make([]model.AppConfigVariable, 0, rt.NumField())
	for i := range rt.NumField() {
		field := rt.Field(i)
		key := field.Tag.Get("json")
		value := rv.FieldByName(field.Name).String()

		// If the emailEnabled is set to false, disable the emailOneTimeAccessEnabled
		if key == s.DbConfig.EmailOneTimeAccessEnabled.Key {
			if rv.FieldByName("EmailEnabled").String() == "false" {
				value = "false"
			}
		}

		var appConfigVariable model.AppConfigVariable
		err = tx.
			WithContext(ctx).
			First(&appConfigVariable, "key = ? AND is_internal = false", key).
			Error
		if err != nil {
			return nil, err
		}

		appConfigVariable.Value = value
		err = tx.
			WithContext(ctx).
			Save(&appConfigVariable).
			Error
		if err != nil {
			return nil, err
		}

		savedConfigVariables = append(savedConfigVariables, appConfigVariable)
	}

	err = tx.Commit().Error
	if err != nil {
		return nil, err
	}

	err = s.LoadDbConfigFromDb()
	if err != nil {
		return nil, err
	}

	return savedConfigVariables, nil
}

func (s *AppConfigService) updateImageType(ctx context.Context, imageName string, fileType string) error {
	key := imageName + "ImageType"
	err := s.db.
		WithContext(ctx).
		Model(&model.AppConfigVariable{}).
		Where("key = ?", key).
		Update("value", fileType).
		Error
	if err != nil {
		return err
	}

	return s.LoadDbConfigFromDb()
}

func (s *AppConfigService) ListAppConfig(ctx context.Context, showAll bool) (configuration []model.AppConfigVariable, err error) {
	if showAll {
		err = s.db.
			WithContext(ctx).
			Find(&configuration).
			Error
	} else {
		err = s.db.
			WithContext(ctx).
			Find(&configuration, "is_public = true").
			Error
	}

	if err != nil {
		return nil, err
	}

	for i := range configuration {
		if common.EnvConfig.UiConfigDisabled {
			// Set the value to the environment variable if the UI config is disabled
			configuration[i].Value = s.getConfigVariableFromEnvironmentVariable(configuration[i].Key, configuration[i].DefaultValue)
		} else if configuration[i].Value == "" && configuration[i].DefaultValue != "" {
			// Set the value to the default value if it is empty
			configuration[i].Value = configuration[i].DefaultValue
		}
	}

	return configuration, nil
}

func (s *AppConfigService) UpdateImage(ctx context.Context, uploadedFile *multipart.FileHeader, imageName string, oldImageType string) (err error) {
	fileType := utils.GetFileExtension(uploadedFile.Filename)
	mimeType := utils.GetImageMimeType(fileType)
	if mimeType == "" {
		return &common.FileTypeNotSupportedError{}
	}

	// Delete the old image if it has a different file type
	if fileType != oldImageType {
		oldImagePath := common.EnvConfig.UploadPath + "/application-images/" + imageName + "." + oldImageType
		err = os.Remove(oldImagePath)
		if err != nil {
			return err
		}
	}

	imagePath := common.EnvConfig.UploadPath + "/application-images/" + imageName + "." + fileType
	err = utils.SaveFile(uploadedFile, imagePath)
	if err != nil {
		return err
	}

	// Update the file type in the database
	err = s.updateImageType(ctx, imageName, fileType)
	if err != nil {
		return err
	}

	return nil
}

// InitDbConfig creates the default configuration values in the database if they do not exist,
// updates existing configurations if they differ from the default, and deletes any configurations
// that are not in the default configuration.
func (s *AppConfigService) InitDbConfig(ctx context.Context) (err error) {
	tx := s.db.Begin()
	defer func() {
		tx.Rollback()
	}()

	// Reflect to get the underlying value of DbConfig and its default configuration
	defaultConfigReflectValue := reflect.ValueOf(defaultDbConfig)
	defaultKeys := make(map[string]struct{})

	// Iterate over the fields of DbConfig
	for i := range defaultConfigReflectValue.NumField() {
		defaultConfigVar := defaultConfigReflectValue.Field(i).Interface().(model.AppConfigVariable)

		defaultKeys[defaultConfigVar.Key] = struct{}{}

		var storedConfigVar model.AppConfigVariable
		err = tx.
			WithContext(ctx).
			First(&storedConfigVar, "key = ?", defaultConfigVar.Key).
			Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// If the configuration does not exist, create it
			err = tx.
				WithContext(ctx).
				Create(&defaultConfigVar).
				Error
			if err != nil {
				return err
			}
			continue
		} else if err != nil {
			return err
		}

		// Update existing configuration if it differs from the default
		if storedConfigVar.Type != defaultConfigVar.Type ||
			storedConfigVar.IsPublic != defaultConfigVar.IsPublic ||
			storedConfigVar.IsInternal != defaultConfigVar.IsInternal ||
			storedConfigVar.DefaultValue != defaultConfigVar.DefaultValue {
			// Set values
			storedConfigVar.Type = defaultConfigVar.Type
			storedConfigVar.IsPublic = defaultConfigVar.IsPublic
			storedConfigVar.IsInternal = defaultConfigVar.IsInternal
			storedConfigVar.DefaultValue = defaultConfigVar.DefaultValue

			err = tx.
				WithContext(ctx).
				Save(&storedConfigVar).
				Error
			if err != nil {
				return err
			}
		}
	}

	// Delete any configurations not in the default keys
	var allConfigVars []model.AppConfigVariable
	err = tx.
		WithContext(ctx).
		Find(&allConfigVars).
		Error
	if err != nil {
		return err
	}

	for _, config := range allConfigVars {
		if _, exists := defaultKeys[config.Key]; exists {
			continue
		}

		err = tx.
			WithContext(ctx).
			Delete(&config).
			Error
		if err != nil {
			return err
		}
	}

	// Commit the changes
	err = tx.Commit().Error
	if err != nil {
		return err
	}

	// Reload the configuration
	err = s.LoadDbConfigFromDb()
	if err != nil {
		return err
	}

	return nil
}

// LoadDbConfigFromDb loads the configuration values from the database into the DbConfig struct.
func (s *AppConfigService) LoadDbConfigFromDb() error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		dbConfigReflectValue := reflect.ValueOf(s.DbConfig).Elem()

		for i := range dbConfigReflectValue.NumField() {
			dbConfigField := dbConfigReflectValue.Field(i)
			currentConfigVar := dbConfigField.Interface().(model.AppConfigVariable)
			var storedConfigVar model.AppConfigVariable
			err := tx.First(&storedConfigVar, "key = ?", currentConfigVar.Key).Error
			if err != nil {
				return err
			}

			if common.EnvConfig.UiConfigDisabled {
				storedConfigVar.Value = s.getConfigVariableFromEnvironmentVariable(currentConfigVar.Key, storedConfigVar.DefaultValue)
			} else if storedConfigVar.Value == "" && storedConfigVar.DefaultValue != "" {
				storedConfigVar.Value = storedConfigVar.DefaultValue
			}

			dbConfigField.Set(reflect.ValueOf(storedConfigVar))
		}

		return nil
	})
}

func (s *AppConfigService) getConfigVariableFromEnvironmentVariable(key, fallbackValue string) string {
	environmentVariableName := utils.CamelCaseToScreamingSnakeCase(key)

	if value, exists := os.LookupEnv(environmentVariableName); exists {
		return value
	}

	return fallbackValue
}
