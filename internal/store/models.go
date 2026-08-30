package store

import (
	"errors"

	"github.com/dvoulgaridis/bulk-mail/internal/mail"
)

type AddressFieldRole string
type ProfileType string

const (
	NewCampaignID                    = int64(-1)
	DefaultEmailRatePerMin           = 60
	DefaultEmailIntervalMs           = 100
	DefaultMaxCampaignAddressEntries = 10000
	DefaultMaxCampaignDocuments      = 5
	MaxEmailRatePerMin               = 10000
	MinEmailIntervalMs               = 100
	MaxEmailIntervalMs               = 3600000
	MaxCampaignAddressEntries        = 10000
	MaxCampaignDocuments             = 5
	MaxAddressListFields             = 32
	CredentialSMTPPassword           = "smtp_password"
	CredentialGmailRefreshToken      = "gmail_refresh_token"
)

const (
	ProfileTypeSMTP             ProfileType = "smtp"
	ProfileTypeGmailAppPassword ProfileType = "gmail_app_password"
	ProfileTypeGmailOAuth       ProfileType = "gmail_oauth"
)

const (
	AddressFieldRoleNone      AddressFieldRole = ""
	AddressFieldRoleEmail     AddressFieldRole = "email"
	AddressFieldRoleFirstName AddressFieldRole = "first_name"
	AddressFieldRoleLastName  AddressFieldRole = "last_name"
)

type AppSettings struct {
	Theme                     string `json:"theme"`
	EmailRatePerMin           int    `json:"emailRatePerMin"`
	EmailIntervalMs           int    `json:"emailIntervalMs"`
	MaxCampaignAddressEntries int    `json:"maxCampaignAddressEntries"`
	MaxCampaignDocuments      int    `json:"maxCampaignDocuments"`
}

func DefaultAppSettings() AppSettings {
	return AppSettings{
		Theme:                     "light",
		EmailRatePerMin:           DefaultEmailRatePerMin,
		EmailIntervalMs:           DefaultEmailIntervalMs,
		MaxCampaignAddressEntries: DefaultMaxCampaignAddressEntries,
		MaxCampaignDocuments:      DefaultMaxCampaignDocuments,
	}
}

func (settings AppSettings) Validate() error {
	if settings.Theme != "light" && settings.Theme != "dark" {
		return errors.New("theme must be light or dark")
	}
	if settings.EmailRatePerMin < 1 || settings.EmailRatePerMin > MaxEmailRatePerMin {
		return errors.New("email rate must be between 1 and 10000 messages per minute")
	}
	if settings.EmailIntervalMs < MinEmailIntervalMs || settings.EmailIntervalMs > MaxEmailIntervalMs {
		return errors.New("email interval must be between 100 and 3600000 milliseconds")
	}
	if settings.MaxCampaignAddressEntries < 1 || settings.MaxCampaignAddressEntries > MaxCampaignAddressEntries {
		return errors.New("maximum campaign address entries must be between 1 and 10000")
	}
	if settings.MaxCampaignDocuments < 1 || settings.MaxCampaignDocuments > MaxCampaignDocuments {
		return errors.New("maximum campaign documents must be between 1 and 5")
	}
	return nil
}

type SMTPProfile struct {
	ID             int64       `json:"id"`
	Name           string      `json:"name"`
	ProfileType    ProfileType `json:"profileType"`
	Host           string      `json:"host"`
	Port           int         `json:"port"`
	TLSMode        string      `json:"tlsMode"`
	Username       string      `json:"username"`
	SenderEmail    string      `json:"senderEmail"`
	SenderName     string      `json:"senderName"`
	ReplyTo        string      `json:"replyTo"`
	PasswordExists bool        `json:"passwordExists"`
	HasGoogleOAuth bool        `json:"hasGoogleOAuth"`
	CreatedAt      string      `json:"createdAt"`
	UpdatedAt      string      `json:"updatedAt"`
}

type ProfileCredential struct {
	ProfileID      int64
	CredentialType string
	Scheme         string
	SealedValue    []byte
}

type AddressList struct {
	ID        int64                    `json:"id"`
	Name      string                   `json:"name"`
	Source    string                   `json:"source"`
	Notes     string                   `json:"notes"`
	Fields    []AddressFieldDefinition `json:"fields"`
	Entries   []AddressEntry           `json:"entries,omitempty"`
	Count     int                      `json:"count"`
	CreatedAt string                   `json:"createdAt"`
	UpdatedAt string                   `json:"updatedAt"`
}

type AddressFieldDefinition struct {
	Key      string           `json:"key"`
	Label    string           `json:"label"`
	Role     AddressFieldRole `json:"role"`
	Position int              `json:"position"`
}

func DefaultAddressFields() []AddressFieldDefinition {
	return []AddressFieldDefinition{
		{
			Key:      string(AddressFieldRoleEmail),
			Label:    "Email",
			Role:     AddressFieldRoleEmail,
			Position: 0,
		},
		{
			Key:      string(AddressFieldRoleFirstName),
			Label:    "First name",
			Role:     AddressFieldRoleFirstName,
			Position: 1,
		},
		{
			Key:      string(AddressFieldRoleLastName),
			Label:    "Last name",
			Role:     AddressFieldRoleLastName,
			Position: 2,
		},
	}
}

type AddressFields map[string]string

type AddressEntry struct {
	ID          int64         `json:"id"`
	Email       string        `json:"email"`
	DisplayName string        `json:"displayName"`
	Fields      AddressFields `json:"fields"`
}

type PersonalizationOptions struct {
	RemoveDiacritics bool   `json:"removeDiacritics"`
	FirstNameFormat  string `json:"firstNameFormat"`
	LastNameFormat   string `json:"lastNameFormat"`
	FullNameFormat   string `json:"fullNameFormat"`
}

type Campaign struct {
	ID              int64                  `json:"id"`
	Name            string                 `json:"name"`
	AddressListID   int64                  `json:"addressListId"`
	ProfileID       *int64                 `json:"profileId"`
	Message         mail.MessageContent    `json:"message"`
	Personalization PersonalizationOptions `json:"personalization"`
	CreatedAt       string                 `json:"createdAt"`
	UpdatedAt       string                 `json:"updatedAt"`
}

type MessageDelivery struct {
	ID                int64  `json:"id"`
	TaskID            int64  `json:"taskId"`
	CampaignID        *int64 `json:"campaignId"`
	AddressEntryID    *int64 `json:"addressEntryId"`
	Email             string `json:"email"`
	Status            string `json:"status"`
	Attempt           int    `json:"attempt"`
	ProviderMessageID string `json:"providerMessageId"`
	LastError         string `json:"lastError"`
	CreatedAt         string `json:"createdAt"`
	UpdatedAt         string `json:"updatedAt"`
}

type Suppression struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	Reason    string `json:"reason"`
	CreatedAt string `json:"createdAt"`
}

type AppState struct {
	Settings             AppSettings              `json:"settings"`
	AddressFieldDefaults []AddressFieldDefinition `json:"addressFieldDefaults"`
	SMTPProfiles         []SMTPProfile            `json:"smtpProfiles"`
	AddressLists         []AddressList            `json:"addressLists"`
	Campaigns            []Campaign               `json:"campaigns"`
	Suppressions         []Suppression            `json:"suppressions"`
}
