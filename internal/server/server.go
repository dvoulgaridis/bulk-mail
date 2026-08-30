package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dvoulgaridis/bulk-mail/internal/app"
	"github.com/dvoulgaridis/bulk-mail/internal/credentials"
	"github.com/dvoulgaridis/bulk-mail/internal/document"
	"github.com/dvoulgaridis/bulk-mail/internal/local"
	"github.com/dvoulgaridis/bulk-mail/internal/store"
	"github.com/dvoulgaridis/bulk-mail/internal/tasks"
)

// Server state and configuration.

type Server struct {
	*http.Server
	repo               *store.Store
	oauth              map[string]oauthAttempt
	oauthMu            sync.Mutex
	credentialKey      []byte
	token              string
	taskQueue          *tasks.Queue
	taskEvents         *taskEventHub
	delivery           *app.DeliveryService
	campaignService    *app.CampaignService
	documentConverter  document.DOCXToPDFConverter
	googleClientID     string
	smtpConnectTimeout time.Duration
	shutdownTimeout    time.Duration
}

type Options struct {
	MaxConcurrentTasks      int
	MaxQueuedTasks          int
	ShutdownTimeout         time.Duration
	SendTimeout             time.Duration
	SMTPConnectTimeout      time.Duration
	GoogleClientID          string
	LibreOfficeExecutable   string
	LibreOfficeBatchTimeout time.Duration
	TemporaryPaths          local.TemporaryPaths
}

type oauthAttempt struct {
	ProfileID   int64
	ProfileName string
	Verifier    string
	RedirectURI string
	ExpiresAt   time.Time
}

// Construction.

func New(repo *store.Store, paths local.Paths, uiFS fs.FS, options Options) (*Server, error) {
	if options.MaxConcurrentTasks < 1 {
		return nil, errors.New("maximum concurrent tasks must be at least 1")
	}
	if options.MaxQueuedTasks < 1 {
		return nil, errors.New("maximum queued tasks must be at least 1")
	}
	if options.ShutdownTimeout <= 0 {
		return nil, errors.New("shutdown timeout must be positive")
	}
	if options.SendTimeout <= 0 {
		return nil, errors.New("send timeout must be positive")
	}
	if options.SMTPConnectTimeout <= 0 {
		return nil, errors.New("SMTP connect timeout must be positive")
	}
	if options.LibreOfficeBatchTimeout <= 0 {
		return nil, errors.New("LibreOffice batch timeout must be positive")
	}
	if err := options.TemporaryPaths.Validate(); err != nil {
		return nil, fmt.Errorf("temporary storage: %w", err)
	}
	token, err := randomToken()
	if err != nil {
		return nil, fmt.Errorf("create local API token: %w", err)
	}
	credentialKeyPath := paths.CredentialKeyPath
	if credentialKeyPath == "" {
		credentialKeyPath = filepath.Join(paths.DataDir, "bulk-mail.key")
	}
	credentialKey, err := credentials.LoadOrCreateKey(credentialKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load credential key: %w", err)
	}
	taskEvents := newTaskEventHub(repo)
	taskQueue, err := tasks.NewQueue(
		context.Background(),
		repo,
		paths.TaskQueueDir,
		options.MaxConcurrentTasks,
		options.MaxQueuedTasks,
		taskEvents.MarkChanged,
	)
	if err != nil {
		taskEvents.Close()
		return nil, fmt.Errorf("create task queue: %w", err)
	}
	googleClientID := strings.TrimSpace(options.GoogleClientID)
	documentConverter := document.DOCXToPDFConverter{
		Executable: strings.TrimSpace(options.LibreOfficeExecutable),
		Workspace:  options.TemporaryPaths.Conversions,
		Timeout:    options.LibreOfficeBatchTimeout,
	}
	preflightConverter := documentConverter
	preflightConverter.Workspace = options.TemporaryPaths.Preflight
	delivery := app.NewDeliveryService(
		repo,
		credentialKey,
		googleClientID,
		options.SendTimeout,
		options.SMTPConnectTimeout,
	)
	campaignService := app.NewCampaignService(
		repo,
		delivery,
		taskQueue,
		documentConverter,
		preflightConverter,
		options.TemporaryPaths.Archives,
		taskEvents.MarkChanged,
	)
	if err := taskQueue.Start(
		campaignService.ExecuteCampaignTask,
		campaignService.RecoverInterruptedCampaignTask,
		campaignService.CancelQueuedCampaignTask,
	); err != nil {
		taskEvents.Close()
		return nil, fmt.Errorf("start task queue: %w", err)
	}
	s := &Server{
		repo:               repo,
		oauth:              map[string]oauthAttempt{},
		credentialKey:      credentialKey,
		token:              token,
		taskQueue:          taskQueue,
		taskEvents:         taskEvents,
		delivery:           delivery,
		campaignService:    campaignService,
		documentConverter:  documentConverter,
		googleClientID:     googleClientID,
		smtpConnectTimeout: options.SMTPConnectTimeout,
		shutdownTimeout:    options.ShutdownTimeout,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/session/bootstrap", s.handleSessionBootstrap)
	mux.HandleFunc("/api/state", s.requireLocalToken(s.handleState))
	mux.HandleFunc("/api/settings", s.requireLocalToken(s.handleSettings))
	mux.HandleFunc("/api/settings/dependencies", s.requireLocalToken(s.handleSettingsDependencies))
	mux.HandleFunc("/api/smtp/test", s.requireLocalToken(s.handleSMTPTest))
	mux.HandleFunc("/api/smtp/profiles", s.requireLocalToken(s.handleSMTPProfiles))
	mux.HandleFunc("/api/smtp/profiles/", s.requireLocalToken(s.handleSMTPProfileByID))
	mux.HandleFunc("/api/smtp/detect", s.requireLocalToken(s.handleSMTPDetect))
	mux.HandleFunc("/api/oauth/google/start", s.requireLocalToken(s.handleGoogleOAuthStart))
	mux.HandleFunc("/api/oauth/google/callback", s.handleGoogleOAuthCallback)
	mux.HandleFunc("/api/address-lists/import", s.requireLocalToken(s.handleImportAddressList))
	mux.HandleFunc("/api/address-lists/", s.requireLocalToken(s.handleAddressListByID))
	mux.HandleFunc("/api/campaigns", s.requireLocalToken(s.handleCampaigns))
	mux.HandleFunc("/api/campaigns/", s.requireLocalToken(s.handleCampaignByID))
	mux.HandleFunc("/api/campaigns/preflight", s.requireLocalToken(s.handleCampaignPreflight))
	mux.HandleFunc("/api/campaigns/generate", s.requireLocalToken(s.handleCampaignGenerate))
	mux.HandleFunc("/api/campaigns/send", s.requireLocalToken(s.handleCampaignSend))
	mux.HandleFunc("/api/tasks/", s.requireLocalToken(s.handleTaskByID))
	mux.HandleFunc("/api/events/tasks", s.requireLocalToken(s.handleTaskEvents))
	mux.HandleFunc("/api/suppressions", s.requireLocalToken(s.handleSuppressions))
	mux.HandleFunc("/api/suppressions/", s.requireLocalToken(s.handleSuppressionByID))
	mux.HandleFunc("/api/app/quit", s.requireLocalToken(s.handleQuit))
	mux.Handle("/", http.FileServer(http.FS(uiFS)))
	s.Server = &http.Server{Handler: s.localOnly(mux)}
	return s, nil
}

// Lifecycle.

func (s *Server) Shutdown(ctx context.Context) error {
	s.taskEvents.Close()
	taskErrors := make(chan error, 1)
	go func() { taskErrors <- s.taskQueue.Shutdown(ctx) }()
	httpErr := s.Server.Shutdown(ctx)
	taskErr := <-taskErrors
	s.campaignService.Close()
	return errors.Join(taskErr, httpErr)
}
