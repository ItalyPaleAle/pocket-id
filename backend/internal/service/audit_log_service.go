package service

import (
	"context"
	"log"

	userAgentParser "github.com/mileusna/useragent"
	"github.com/pocket-id/pocket-id/backend/internal/model"
	"github.com/pocket-id/pocket-id/backend/internal/utils"
	"github.com/pocket-id/pocket-id/backend/internal/utils/email"
	"gorm.io/gorm"
)

type AuditLogService struct {
	db               *gorm.DB
	appConfigService *AppConfigService
	emailService     *EmailService
	geoliteService   *GeoLiteService
}

func NewAuditLogService(db *gorm.DB, appConfigService *AppConfigService, emailService *EmailService, geoliteService *GeoLiteService) *AuditLogService {
	return &AuditLogService{db: db, appConfigService: appConfigService, emailService: emailService, geoliteService: geoliteService}
}

// Create creates a new audit log entry in the database
func (s *AuditLogService) Create(ctx context.Context, event model.AuditLogEvent, ipAddress, userAgent, userID string, data model.AuditLogData, tx *gorm.DB) model.AuditLog {
	country, city, err := s.geoliteService.GetLocationByIP(ipAddress)
	if err != nil {
		log.Printf("Failed to get IP location: %v", err)
	}

	auditLog := model.AuditLog{
		Event:     event,
		IpAddress: ipAddress,
		Country:   country,
		City:      city,
		UserAgent: userAgent,
		UserID:    userID,
		Data:      data,
	}

	// Save the audit log in the database
	err = tx.
		WithContext(ctx).
		Create(&auditLog).
		Error
	if err != nil {
		log.Printf("Failed to create audit log: %v", err)
		return model.AuditLog{}
	}

	return auditLog
}

// CreateNewSignInWithEmail creates a new audit log entry in the database and sends an email if the device hasn't been used before
func (s *AuditLogService) CreateNewSignInWithEmail(ctx context.Context, ipAddress, userAgent, userID string, tx *gorm.DB) model.AuditLog {
	createdAuditLog := s.Create(ctx, model.AuditLogEventSignIn, ipAddress, userAgent, userID, model.AuditLogData{}, tx)

	// Count the number of times the user has logged in from the same device
	var count int64
	err := tx.
		WithContext(ctx).
		Model(&model.AuditLog{}).
		Where("user_id = ? AND ip_address = ? AND user_agent = ?", userID, ipAddress, userAgent).
		Count(&count).
		Error
	if err != nil {
		log.Printf("Failed to count audit logs: %v\n", err)
		return createdAuditLog
	}

	// If the user hasn't logged in from the same device before and email notifications are enabled, send an email
	if s.appConfigService.DbConfig.EmailLoginNotificationEnabled.IsTrue() && count <= 1 {
		// We use a background context here as this is running in a goroutine
		//nolint:contextcheck
		go func() {
			innerCtx := context.Background()

			// Note we don't use the transaction here because this is running in background
			var user model.User
			innerErr := s.db.
				WithContext(innerCtx).
				Where("id = ?", userID).
				First(&user).
				Error
			if innerErr != nil {
				log.Printf("Failed to load user: %v", innerErr)
			}

			innerErr = SendEmail(innerCtx, s.emailService, email.Address{
				Name:  user.Username,
				Email: user.Email,
			}, NewLoginTemplate, &NewLoginTemplateData{
				IPAddress: ipAddress,
				Country:   createdAuditLog.Country,
				City:      createdAuditLog.City,
				Device:    s.DeviceStringFromUserAgent(userAgent),
				DateTime:  createdAuditLog.CreatedAt.UTC(),
			})
			if innerErr != nil {
				log.Printf("Failed to send email to '%s': %v", user.Email, innerErr)
			}
		}()
	}

	return createdAuditLog
}

// ListAuditLogsForUser retrieves all audit logs for a given user ID
func (s *AuditLogService) ListAuditLogsForUser(ctx context.Context, userID string, sortedPaginationRequest utils.SortedPaginationRequest) ([]model.AuditLog, utils.PaginationResponse, error) {
	var logs []model.AuditLog
	query := s.db.
		WithContext(ctx).
		Model(&model.AuditLog{}).
		Where("user_id = ?", userID)

	pagination, err := utils.PaginateAndSort(sortedPaginationRequest, query, &logs)
	return logs, pagination, err
}

func (s *AuditLogService) DeviceStringFromUserAgent(userAgent string) string {
	ua := userAgentParser.Parse(userAgent)
	return ua.Name + " on " + ua.OS + " " + ua.OSVersion
}
