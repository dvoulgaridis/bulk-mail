package app

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dvoulgaridis/bulk-mail/internal/document"
	"github.com/dvoulgaridis/bulk-mail/internal/mail"
	"github.com/dvoulgaridis/bulk-mail/internal/store"
	"github.com/dvoulgaridis/bulk-mail/internal/tasks"
	"github.com/dvoulgaridis/bulk-mail/internal/templates"
)

const (
	maxDeliveryAttempts = 3
)

// Campaign commands, snapshots, and results.

type SaveCampaignCommand struct {
	Name            string                       `json:"name"`
	AddressListID   int64                        `json:"addressListId"`
	ProfileID       *int64                       `json:"profileId"`
	Message         mail.MessageContent          `json:"message"`
	Personalization store.PersonalizationOptions `json:"personalization"`
}

type PreflightCampaignCommand struct {
	Mode                 string                       `json:"mode"`
	AddressListID        int64                        `json:"addressListId"`
	Message              mail.MessageContent          `json:"message"`
	Personalization      store.PersonalizationOptions `json:"personalization"`
	SampleAddressEntryID int64                        `json:"sampleAddressEntryId"`
}

type ExecuteCampaignCommand struct {
	CampaignID          int64    `json:"campaignId"`
	ConfirmedUnresolved []string `json:"confirmedUnresolved"`
}

type preparedCampaign struct {
	CampaignTaskSnapshot
	Documents []document.CampaignTemplate
}

type MessagePreview struct {
	AddressEntryID int64  `json:"addressEntryId"`
	Email          string `json:"email"`
	Name           string `json:"name"`
	Subject        string `json:"subject"`
	Body           string `json:"body"`
	HTMLBody       string `json:"htmlBody"`
}

type UnresolvedPlaceholder struct {
	Key       string   `json:"key"`
	Reason    string   `json:"reason"`
	Locations []string `json:"locations"`
}

type PreflightAttachmentInfo struct {
	Filename       string   `json:"filename"`
	Placeholders   []string `json:"placeholders"`
	ConvertedToPDF bool     `json:"convertedToPdf"`
}

type PreflightAttachment struct {
	Filename      string `json:"filename"`
	ContentType   string `json:"contentType"`
	Size          int    `json:"size"`
	PageCount     int    `json:"pageCount"`
	ContentBase64 string `json:"contentBase64"`
}

type PreflightSample struct {
	MessagePreview
	Attachments []PreflightAttachment `json:"attachments"`
}

type PreflightResult struct {
	Count              int                       `json:"count"`
	Attachments        []PreflightAttachmentInfo `json:"attachments"`
	Unresolved         []UnresolvedPlaceholder   `json:"unresolved"`
	Confirmation       []string                  `json:"confirmation"`
	Samples            []PreflightSample         `json:"samples"`
	LibreOfficeChecked bool                      `json:"libreOfficeChecked"`
}

type GeneratedArchive struct {
	Path     string
	Filename string
	Cleanup  func()
}

// Campaign service dependencies.

type settingsReader interface {
	GetAppSettings(context.Context) (store.AppSettings, error)
}

type addressListReader interface {
	GetAddressList(context.Context, int64) (store.AddressList, error)
}

type executionStore interface {
	SaveCampaign(context.Context, store.Campaign) (store.Campaign, error)
	GetCampaign(context.Context, int64) (store.Campaign, error)
	UpdateTaskStatus(context.Context, int64, string) error
	CreateDelivery(context.Context, store.MessageDelivery) (store.MessageDelivery, error)
	UpdateDeliveryAttempt(context.Context, int64, int, string) error
	UpdateDeliveryStatus(context.Context, int64, string, string, string) error
	IncrementTaskSent(context.Context, int64) error
	IncrementTaskFailed(context.Context, int64) error
	IncrementTaskSkipped(context.Context, int64) error
	GetTask(context.Context, int64) (tasks.Task, error)
	FinishTask(context.Context, int64, string, string) error
	FinalizeInterruptedTask(context.Context, int64, []string, string) error
	CancelQueuedCampaignTask(context.Context, int64, []string) (bool, error)
}

type suppressionReader interface {
	IsSuppressed(context.Context, string) (bool, error)
}

type CampaignService struct {
	settings      settingsReader
	addressLists  addressListReader
	execution     executionStore
	suppressions  suppressionReader
	delivery      *DeliveryService
	taskQueue     *tasks.Queue
	converter     document.DOCXToPDFConverter
	preflight     document.DOCXToPDFConverter
	preflightGate chan struct{}
	archiveDir    string
	notifyTask    func(int64)
	archivesMu    sync.Mutex
	archives      map[int64]GeneratedArchive
}

// Campaign service construction.

func NewCampaignService(
	repo *store.Store,
	delivery *DeliveryService,
	taskQueue *tasks.Queue,
	converter document.DOCXToPDFConverter,
	preflight document.DOCXToPDFConverter,
	archiveDir string,
	notifyTask func(int64),
) *CampaignService {
	return &CampaignService{
		settings:      repo,
		addressLists:  repo,
		execution:     repo,
		suppressions:  repo,
		delivery:      delivery,
		taskQueue:     taskQueue,
		converter:     converter,
		preflight:     preflight,
		preflightGate: make(chan struct{}, 1),
		archiveDir:    archiveDir,
		notifyTask:    notifyTask,
		archives:      map[int64]GeneratedArchive{},
	}
}

// Campaign persistence and preflight.

func (service *CampaignService) SaveCampaign(
	ctx context.Context,
	id int64,
	command SaveCampaignCommand,
) (store.Campaign, error) {
	campaign := store.Campaign{
		ID:              id,
		Name:            command.Name,
		AddressListID:   command.AddressListID,
		ProfileID:       command.ProfileID,
		Message:         command.Message,
		Personalization: command.Personalization,
	}
	campaign.Name = strings.TrimSpace(campaign.Name)
	if campaign.ID != store.NewCampaignID && campaign.ID <= 0 {
		return store.Campaign{}, failure(ErrorValidation, "campaign id must be -1 or positive", nil)
	}
	if campaign.Name == "" {
		return store.Campaign{}, failure(ErrorValidation, "campaign name is required", nil)
	}
	if campaign.AddressListID <= 0 {
		return store.Campaign{}, failure(ErrorValidation, "address list is required", nil)
	}
	if campaign.ProfileID != nil && *campaign.ProfileID <= 0 {
		return store.Campaign{}, failure(ErrorValidation, "profile id must be positive", nil)
	}
	if _, err := service.addressLists.GetAddressList(ctx, campaign.AddressListID); errors.Is(err, sql.ErrNoRows) {
		return store.Campaign{}, failure(ErrorNotFound, "address list not found", err)
	} else if err != nil {
		return store.Campaign{}, internalFailure("load address list", err)
	}
	if campaign.ProfileID != nil {
		if _, err := service.delivery.Profile(ctx, *campaign.ProfileID); err != nil {
			return store.Campaign{}, err
		}
	}
	settings, err := service.settings.GetAppSettings(ctx)
	if err != nil {
		return store.Campaign{}, internalFailure("load application settings", err)
	}
	if _, _, err := validateAttachments(
		campaign.Message.Attachments,
		settings.MaxCampaignDocuments,
	); err != nil {
		return store.Campaign{}, err
	}
	if err := validatePersonalization(campaign.Personalization); err != nil {
		return store.Campaign{}, err
	}
	campaign.Personalization.FirstNameFormat = normalizedFormat(
		campaign.Personalization.FirstNameFormat,
	)
	campaign.Personalization.LastNameFormat = normalizedFormat(
		campaign.Personalization.LastNameFormat,
	)
	campaign.Personalization.FullNameFormat = normalizedFormat(
		campaign.Personalization.FullNameFormat,
	)
	for index := range campaign.Message.Attachments {
		attachment := &campaign.Message.Attachments[index]
		attachment.Filename = safeFilename(attachment.Filename)
		attachment.ContentType = ""
		attachment.Size = len(attachment.Content)
	}
	campaign.CreatedAt = ""
	campaign.UpdatedAt = ""
	saved, err := service.execution.SaveCampaign(ctx, campaign)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Campaign{}, failure(ErrorNotFound, "campaign not found", err)
	}
	if err != nil {
		return store.Campaign{}, internalFailure("save campaign", err)
	}
	return saved, nil
}

func (service *CampaignService) Preflight(
	ctx context.Context,
	command PreflightCampaignCommand,
) (PreflightResult, error) {
	campaign, result, err := service.validateCampaign(ctx, command)
	if err != nil {
		return PreflightResult{}, err
	}
	select {
	case service.preflightGate <- struct{}{}:
		defer func() { <-service.preflightGate }()
	case <-ctx.Done():
		return PreflightResult{}, ctx.Err()
	}
	budget := newAttachmentBudget()
	staticPDFs, sharedBytes, err := prepareSharedAttachments(
		ctx,
		service.preflight,
		campaign.Campaign.Message.Attachments,
		campaign.Documents,
		budget,
	)
	if err != nil {
		return PreflightResult{}, failure(ErrorProcessing, fmt.Sprintf("attachment preparation failed: %v", err), err)
	}
	defer budget.release(sharedBytes)
	for _, entry := range sampleEntries(campaign.AddressList.Entries, command.SampleAddressEntryID) {
		fields := personalizedFields(entry, campaign.Campaign.Personalization)
		sample := PreflightSample{
			MessagePreview: messagePreview(entry, fields, campaign.Campaign.Message),
			Attachments:    []PreflightAttachment{},
		}
		attachments, reserved, err := prepareAddressEntryAttachments(
			ctx,
			service.preflight,
			campaign.Campaign.Message.Attachments,
			campaign.Documents,
			fields,
			staticPDFs,
			budget,
		)
		if err != nil {
			return PreflightResult{}, failure(
				ErrorProcessing,
				fmt.Sprintf("sample for %s failed: %v", entry.Email, err),
				err,
			)
		}
		for _, attachment := range attachments {
			sample.Attachments = append(sample.Attachments, PreflightAttachment{
				Filename:      attachment.Filename,
				ContentType:   attachment.ContentType,
				Size:          len(attachment.Content),
				PageCount:     attachmentPageCount(attachment),
				ContentBase64: base64.StdEncoding.EncodeToString(attachment.Content),
			})
		}
		budget.release(reserved)
		result.Samples = append(result.Samples, sample)
	}
	return result, nil
}

func (service *CampaignService) validateCampaign(
	ctx context.Context,
	command PreflightCampaignCommand,
) (preparedCampaign, PreflightResult, error) {
	if command.Mode != "send" && command.Mode != "generate" {
		return preparedCampaign{}, PreflightResult{}, failure(
			ErrorValidation,
			"campaign mode must be send or generate",
			nil,
		)
	}
	campaign, err := service.prepare(ctx, store.Campaign{
		AddressListID:   command.AddressListID,
		Message:         command.Message,
		Personalization: command.Personalization,
	})
	if err != nil {
		return preparedCampaign{}, PreflightResult{}, err
	}
	return validatePreparedCampaign(campaign, command.Mode)
}

func validatePreparedCampaign(
	campaign preparedCampaign,
	mode string,
) (preparedCampaign, PreflightResult, error) {
	if mode == "generate" && len(campaign.Documents) == 0 {
		return preparedCampaign{}, PreflightResult{}, failure(
			ErrorValidation,
			"choose at least one DOCX document",
			nil,
		)
	}
	result := PreflightResult{
		Count:              len(campaign.AddressList.Entries),
		Attachments:        []PreflightAttachmentInfo{},
		Unresolved:         []UnresolvedPlaceholder{},
		Confirmation:       []string{},
		Samples:            []PreflightSample{},
		LibreOfficeChecked: len(campaign.Documents) > 0,
	}
	locations := map[string]map[string]bool{}
	addLocations(locations, "subject", templates.Keys(campaign.Campaign.Message.Subject))
	addLocations(locations, "message body", templates.Keys(campaign.Campaign.Message.Body))
	addLocations(locations, "HTML body", templates.Keys(campaign.Campaign.Message.HTMLBody))
	documentID := 0
	for _, attachment := range campaign.Campaign.Message.Attachments {
		info := PreflightAttachmentInfo{
			Filename:     attachment.Filename,
			Placeholders: []string{},
		}
		if isDOCXFilename(attachment.Filename) {
			input := campaign.Documents[documentID]
			info.Placeholders = input.Placeholders()
			info.ConvertedToPDF = true
			addLocations(locations, "document "+input.Filename, info.Placeholders)
			addLocations(locations, "output filename for "+input.Filename, templates.Keys(input.OutputFilename))
			documentID++
		}
		result.Attachments = append(result.Attachments, info)
	}
	available := map[string]bool{}
	for _, field := range campaign.AddressList.Fields {
		available[field.Key] = true
	}
	available["email"] = true
	available["full_name"] = true
	for _, key := range sortedLocationKeys(locations) {
		reason := ""
		if !available[key] {
			reason = "missing_field"
		} else if neverPopulated(
			key,
			campaign.AddressList.Entries,
			campaign.Campaign.Personalization,
		) {
			reason = "never_populated"
		}
		if reason == "" {
			continue
		}
		issue := UnresolvedPlaceholder{Key: key, Reason: reason, Locations: mapKeys(locations[key])}
		result.Unresolved = append(result.Unresolved, issue)
		result.Confirmation = append(result.Confirmation, confirmationValue(issue))
	}
	return campaign, result, nil
}

// Generated archive access and cleanup.

func (service *CampaignService) TakeGeneratedArchive(ctx context.Context, taskID int64) (GeneratedArchive, error) {
	task, err := service.execution.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return GeneratedArchive{}, failure(ErrorNotFound, "task not found", err)
		}
		return GeneratedArchive{}, internalFailure("load task", err)
	}
	if task.Status != "completed" && task.Status != "completed_with_errors" {
		return GeneratedArchive{}, failure(ErrorValidation, "generated archive is not ready", nil)
	}
	service.archivesMu.Lock()
	archive, ok := service.archives[taskID]
	if ok {
		delete(service.archives, taskID)
	}
	service.archivesMu.Unlock()
	if !ok {
		return GeneratedArchive{}, failure(ErrorNotFound, "generated archive is no longer available", nil)
	}
	return archive, nil
}

func (service *CampaignService) ArchiveAvailable(taskID int64) bool {
	service.archivesMu.Lock()
	defer service.archivesMu.Unlock()
	_, available := service.archives[taskID]
	return available
}

func (service *CampaignService) Close() {
	service.archivesMu.Lock()
	defer service.archivesMu.Unlock()
	for id, archive := range service.archives {
		if archive.Cleanup != nil {
			archive.Cleanup()
		}
		delete(service.archives, id)
	}
}

// Campaign preparation.

func (service *CampaignService) prepare(
	ctx context.Context,
	campaign store.Campaign,
) (preparedCampaign, error) {
	settings, err := service.settings.GetAppSettings(ctx)
	if err != nil {
		return preparedCampaign{}, internalFailure("load application settings", err)
	}
	list, err := service.loadAddressList(ctx, campaign.AddressListID)
	if err != nil {
		return preparedCampaign{}, err
	}
	return prepareCampaign(CampaignTaskSnapshot{
		Campaign:    campaign,
		Settings:    settings,
		AddressList: list,
	})
}

func prepareCampaign(snapshot CampaignTaskSnapshot) (preparedCampaign, error) {
	campaign := preparedCampaign{CampaignTaskSnapshot: snapshot}
	if err := validateCampaignRuntime(snapshot); err != nil {
		return preparedCampaign{}, err
	}
	attachments, documents, err := validateAttachments(
		campaign.Campaign.Message.Attachments,
		campaign.Settings.MaxCampaignDocuments,
	)
	if err != nil {
		return preparedCampaign{}, err
	}
	campaign.Campaign.Message.Attachments = attachments
	campaign.Documents = documents
	return campaign, nil
}

func validateCampaignRuntime(snapshot CampaignTaskSnapshot) error {
	if err := snapshot.Settings.Validate(); err != nil {
		return failure(ErrorValidation, err.Error(), err)
	}
	if err := validatePersonalization(snapshot.Campaign.Personalization); err != nil {
		return err
	}
	if len(snapshot.AddressList.Entries) > snapshot.Settings.MaxCampaignAddressEntries {
		return campaignSizeError(
			len(snapshot.AddressList.Entries),
			snapshot.Settings.MaxCampaignAddressEntries,
		)
	}
	return nil
}

func (service *CampaignService) loadAddressList(
	ctx context.Context,
	listID int64,
) (store.AddressList, error) {
	if listID <= 0 {
		return store.AddressList{}, failure(ErrorValidation, "address list is required", nil)
	}
	list, err := service.addressLists.GetAddressList(ctx, listID)
	if errors.Is(err, sql.ErrNoRows) {
		return store.AddressList{}, failure(ErrorNotFound, "address list not found", err)
	}
	if err != nil {
		return store.AddressList{}, internalFailure("load address list", err)
	}
	if len(list.Entries) == 0 {
		return store.AddressList{}, failure(ErrorValidation, "address list has no entries", nil)
	}
	return list, nil
}

// Email execution and retry policy.

type preparedSend struct {
	taskID   int64
	sender   mail.Sender
	campaign preparedCampaign
}

type campaignItem struct {
	Index               int
	Entry               store.AddressEntry
	Fields              map[string]string
	Attachments         []mail.Attachment
	ReservedBytes       int64
	InitiallySuppressed bool
	Err                 error
}

type campaignPacer struct {
	minimumInterval time.Duration
	lastSendAt      time.Time
}

func newCampaignPacer(settings store.AppSettings) campaignPacer {
	rateInterval := time.Minute / time.Duration(settings.EmailRatePerMin)
	configuredInterval := time.Duration(settings.EmailIntervalMs) * time.Millisecond
	return campaignPacer{minimumInterval: max(rateInterval, configuredInterval)}
}

func (pacer *campaignPacer) Wait(ctx context.Context) bool {
	if pacer.lastSendAt.IsZero() {
		return true
	}
	return waitForRate(ctx, time.Until(pacer.lastSendAt.Add(pacer.minimumInterval)))
}

func (pacer *campaignPacer) RecordSend() {
	pacer.lastSendAt = time.Now()
}

func (service *CampaignService) executeSend(ctx context.Context, run preparedSend) {
	defer closeSender(run.sender)
	if !service.beginExecution(ctx, run.taskID, run.campaign) {
		return
	}
	budget := newAttachmentBudget()
	staticPDFs, sharedBytes, err := prepareSharedAttachments(
		ctx,
		service.converter,
		run.campaign.Campaign.Message.Attachments,
		run.campaign.Documents,
		budget,
	)
	if err != nil {
		if ctx.Err() != nil {
			service.finishTerminated(
				ctx,
				run.campaign.Campaign.ID,
				run.taskID,
				run.campaign.AddressList.Entries,
			)
			return
		}
		service.recordRemaining(
			context.Background(),
			run.taskID,
			run.campaign.Campaign.ID,
			run.campaign.AddressList.Entries,
			"failed_processing",
			safeDiagnostic(err.Error()),
		)
		service.finishCompleted(run.taskID, err.Error())
		return
	}
	defer budget.release(sharedBytes)
	pipelineContext, stopPipeline := context.WithCancel(ctx)
	defer stopPipeline()
	ready := make(chan campaignItem, 2)
	admission := make(chan struct{}, 3)
	go service.prepareCampaignItems(
		pipelineContext,
		run.campaign,
		staticPDFs,
		budget,
		admission,
		ready,
	)

	pacer := newCampaignPacer(run.campaign.Settings)
	nextIndex := 0
	for item := range ready {
		nextIndex = item.Index + 1
		if ctx.Err() != nil {
			releaseCampaignItem(&item, budget, admission)
			stopPipeline()
			drainCampaignItems(ready, budget, admission)
			service.finishTerminated(
				ctx,
				run.campaign.Campaign.ID,
				run.taskID,
				run.campaign.AddressList.Entries[item.Index:],
			)
			return
		}
		delivery, err := service.execution.CreateDelivery(ctx, store.MessageDelivery{
			TaskID:         run.taskID,
			CampaignID:     optionalID(run.campaign.Campaign.ID),
			AddressEntryID: optionalID(item.Entry.ID),
			Email:          item.Entry.Email,
			Status:         "attempted",
			Attempt:        1,
		})
		if err != nil {
			releaseCampaignItem(&item, budget, admission)
			if ctx.Err() != nil {
				stopPipeline()
				drainCampaignItems(ready, budget, admission)
				service.finishTerminated(
					ctx,
					run.campaign.Campaign.ID,
					run.taskID,
					run.campaign.AddressList.Entries[item.Index:],
				)
				return
			}
			service.recordFailure(run.taskID, 0, "failed_processing", err)
			continue
		}
		if item.Err != nil {
			releaseCampaignItem(&item, budget, admission)
			if ctx.Err() != nil {
				service.markDeliveryTerminated(delivery.ID, run.taskID, ctx)
				stopPipeline()
				drainCampaignItems(ready, budget, admission)
				service.finishTerminated(
					ctx,
					run.campaign.Campaign.ID,
					run.taskID,
					run.campaign.AddressList.Entries[item.Index+1:],
				)
				return
			}
			service.recordFailure(
				run.taskID,
				delivery.ID,
				"failed_processing",
				item.Err,
			)
			continue
		}
		suppressed, err := service.suppressions.IsSuppressed(ctx, item.Entry.Email)
		if err != nil {
			releaseCampaignItem(&item, budget, admission)
			if ctx.Err() != nil {
				service.markDeliveryTerminated(delivery.ID, run.taskID, ctx)
				stopPipeline()
				drainCampaignItems(ready, budget, admission)
				service.finishTerminated(
					ctx,
					run.campaign.Campaign.ID,
					run.taskID,
					run.campaign.AddressList.Entries[item.Index+1:],
				)
				return
			}
			service.recordFailure(run.taskID, delivery.ID, "failed_processing", err)
			continue
		}
		if suppressed {
			releaseCampaignItem(&item, budget, admission)
			_ = service.execution.UpdateDeliveryStatus(
				ctx,
				delivery.ID,
				"skipped_suppressed",
				"",
				"email address is suppressed",
			)
			_ = service.incrementTaskSkipped(ctx, run.taskID)
			continue
		}
		if item.InitiallySuppressed {
			item.Attachments, item.ReservedBytes, err = prepareAddressEntryAttachments(
				ctx,
				service.converter,
				run.campaign.Campaign.Message.Attachments,
				run.campaign.Documents,
				item.Fields,
				staticPDFs,
				budget,
			)
		}
		if err != nil {
			releaseCampaignItem(&item, budget, admission)
			if ctx.Err() != nil {
				service.markDeliveryTerminated(delivery.ID, run.taskID, ctx)
				stopPipeline()
				drainCampaignItems(ready, budget, admission)
				service.finishTerminated(
					ctx,
					run.campaign.Campaign.ID,
					run.taskID,
					run.campaign.AddressList.Entries[item.Index+1:],
				)
				return
			}
			service.recordFailure(
				run.taskID,
				delivery.ID,
				"failed_processing",
				err,
			)
			continue
		}
		message := withSignature(mail.Message{
			ToEmail: item.Entry.Email,
			ToName:  personalizedName(item.Entry, item.Fields),
			MessageContent: mail.MessageContent{
				Subject: templates.RenderText(run.campaign.Campaign.Message.Subject, item.Fields),
				Body:    templates.RenderText(run.campaign.Campaign.Message.Body, item.Fields),
				HTMLBody: func() string {
					if strings.TrimSpace(run.campaign.Campaign.Message.HTMLBody) == "" {
						return ""
					}
					return templates.RenderHTML(run.campaign.Campaign.Message.HTMLBody, item.Fields)
				}(),
				RequestDeliveryNotice: run.campaign.Campaign.Message.RequestDeliveryNotice,
				Attachments:           item.Attachments,
			},
		})
		result, attempt, kind, err := service.sendWithRetry(ctx, delivery.ID, run.sender, message, &pacer)
		releaseCampaignItem(&item, budget, admission)
		if err != nil {
			if kind == mail.ErrorCancelled || ctx.Err() != nil {
				service.markDeliveryTerminated(delivery.ID, run.taskID, ctx)
				stopPipeline()
				drainCampaignItems(ready, budget, admission)
				service.finishTerminated(
					ctx,
					run.campaign.Campaign.ID,
					run.taskID,
					run.campaign.AddressList.Entries[item.Index+1:],
				)
				return
			}
			service.recordFailureWithProvider(
				run.taskID,
				delivery.ID,
				"failed_"+string(kind),
				result.ProviderMessageID,
				err,
			)
			if kind == mail.ErrorConfiguration {
				stopPipeline()
				drainCampaignItems(ready, budget, admission)
				service.recordRemaining(
					context.Background(),
					run.taskID,
					run.campaign.Campaign.ID,
					run.campaign.AddressList.Entries[item.Index+1:],
					"not_attempted_configuration",
					"delivery stopped after a provider configuration failure",
				)
				service.finishCompleted(run.taskID, err.Error())
				return
			}
		} else {
			_ = service.execution.UpdateDeliveryStatus(ctx, delivery.ID, "sent", result.ProviderMessageID, "")
			_ = service.incrementTaskSent(ctx, run.taskID)
		}
		_ = attempt
	}
	if ctx.Err() != nil {
		service.finishTerminated(
			ctx,
			run.campaign.Campaign.ID,
			run.taskID,
			run.campaign.AddressList.Entries[nextIndex:],
		)
		return
	}
	service.finishCompleted(run.taskID, "")
}

func (service *CampaignService) prepareCampaignItems(
	ctx context.Context,
	campaign preparedCampaign,
	staticPDFs []document.GeneratedPDF,
	budget *attachmentBudget,
	admission chan struct{},
	ready chan<- campaignItem,
) {
	defer close(ready)
	for index, entry := range campaign.AddressList.Entries {
		if ctx.Err() != nil {
			return
		}
		select {
		case admission <- struct{}{}:
		case <-ctx.Done():
			return
		}
		fields := personalizedFields(entry, campaign.Campaign.Personalization)
		item := campaignItem{
			Index:  index,
			Entry:  entry,
			Fields: fields,
		}
		item.InitiallySuppressed, item.Err = service.suppressions.IsSuppressed(ctx, entry.Email)
		if item.Err == nil && !item.InitiallySuppressed {
			item.Attachments, item.ReservedBytes, item.Err = prepareAddressEntryAttachments(
				ctx,
				service.converter,
				campaign.Campaign.Message.Attachments,
				campaign.Documents,
				fields,
				staticPDFs,
				budget,
			)
		}
		select {
		case ready <- item:
		case <-ctx.Done():
			releaseCampaignItem(&item, budget, admission)
			return
		}
	}
}

func releaseCampaignItem(item *campaignItem, budget *attachmentBudget, admission chan struct{}) {
	item.Attachments = nil
	budget.release(item.ReservedBytes)
	item.ReservedBytes = 0
	<-admission
}

func drainCampaignItems(ready <-chan campaignItem, budget *attachmentBudget, admission chan struct{}) {
	for item := range ready {
		releaseCampaignItem(&item, budget, admission)
	}
}

func (service *CampaignService) sendWithRetry(
	ctx context.Context,
	deliveryID int64,
	sender mail.Sender,
	message mail.Message,
	pacer *campaignPacer,
) (mail.SendResult, int, mail.ErrorKind, error) {
	var result mail.SendResult
	var err error
	for attempt := 1; attempt <= maxDeliveryAttempts; attempt++ {
		if attempt > 1 {
			backoff := time.Duration(1<<(attempt-2)) * time.Second
			if !waitForRate(ctx, backoff) {
				return result, attempt, mail.ErrorCancelled, ctx.Err()
			}
			_ = service.execution.UpdateDeliveryAttempt(contextForStatus(ctx), deliveryID, attempt, "retrying")
		}
		if !pacer.Wait(ctx) {
			return result, attempt, mail.ErrorCancelled, ctx.Err()
		}
		result, err = service.delivery.send(ctx, sender, message)
		pacer.RecordSend()
		if err == nil {
			return result, attempt, "", nil
		}
		kind := mail.ClassifyError(err)
		if kind != mail.ErrorTransient || attempt == maxDeliveryAttempts {
			return result, attempt, kind, err
		}
	}
	return result, maxDeliveryAttempts, mail.ErrorTransient, err
}

// Document generation

type preparedGeneration struct {
	taskID   int64
	campaign preparedCampaign
}

func (service *CampaignService) executeGeneration(ctx context.Context, run preparedGeneration) {
	if !service.beginExecution(ctx, run.taskID, run.campaign) {
		return
	}
	budget := newAttachmentBudget()
	staticPDFs, sharedBytes, err := prepareSharedAttachments(
		ctx,
		service.converter,
		run.campaign.Campaign.Message.Attachments,
		run.campaign.Documents,
		budget,
	)
	if err != nil {
		if ctx.Err() != nil {
			service.finishTerminated(
				ctx,
				run.campaign.Campaign.ID,
				run.taskID,
				run.campaign.AddressList.Entries,
			)
			return
		}
		service.recordRemaining(
			context.Background(),
			run.taskID,
			run.campaign.Campaign.ID,
			run.campaign.AddressList.Entries,
			"failed_processing",
			safeDiagnostic(err.Error()),
		)
		service.finishCompleted(run.taskID, err.Error())
		return
	}
	defer budget.release(sharedBytes)
	addressEntries := make([]document.CampaignAddressEntry, 0, len(run.campaign.AddressList.Entries))
	for _, entry := range run.campaign.AddressList.Entries {
		fields := personalizedFields(entry, run.campaign.Campaign.Personalization)
		addressEntries = append(addressEntries, document.CampaignAddressEntry{
			Email:       entry.Email,
			DisplayName: personalizedName(entry, fields),
			Values:      fields,
		})
	}
	processedEntries := 0
	archivePath := filepath.Join(service.archiveDir, fmt.Sprintf("task-%d.zip", run.taskID))
	cleanup, err := document.GenerateCampaignArchive(
		ctx,
		service.converter,
		archivePath,
		addressEntries,
		run.campaign.Documents,
		archiveStaticAttachments(run.campaign.Campaign.Message.Attachments),
		staticPDFs,
		func(result document.GenerationResult) error {
			entry := run.campaign.AddressList.Entries[processedEntries]
			delivery, createErr := service.execution.CreateDelivery(contextForStatus(ctx), store.MessageDelivery{
				TaskID:         run.taskID,
				CampaignID:     optionalID(run.campaign.Campaign.ID),
				AddressEntryID: optionalID(entry.ID),
				Email:          result.Email,
				Status:         result.Status,
				Attempt:        1,
				LastError:      safeDiagnostic(result.Error),
			})
			_ = delivery
			if createErr != nil {
				return createErr
			}
			processedEntries++
			if result.Status == "generated" {
				return service.incrementTaskSent(contextForStatus(ctx), run.taskID)
			}
			return service.incrementTaskFailed(contextForStatus(ctx), run.taskID)
		},
	)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		if ctx.Err() != nil {
			service.finishTerminated(
				ctx,
				run.campaign.Campaign.ID,
				run.taskID,
				run.campaign.AddressList.Entries[processedEntries:],
			)
			return
		}
		service.recordRemaining(
			context.Background(),
			run.taskID,
			run.campaign.Campaign.ID,
			run.campaign.AddressList.Entries[processedEntries:],
			"failed_processing",
			safeDiagnostic(err.Error()),
		)
		service.finishCompleted(run.taskID, err.Error())
		return
	}
	service.archivesMu.Lock()
	service.archives[run.taskID] = GeneratedArchive{
		Path:     archivePath,
		Filename: archiveDownloadName(run.campaign.Campaign.Name),
		Cleanup:  cleanup,
	}
	service.archivesMu.Unlock()
	service.finishCompleted(run.taskID, "")
}

// Task lifecycle and outcome recording.

func (service *CampaignService) beginExecution(
	ctx context.Context,
	taskID int64,
	campaign preparedCampaign,
) bool {
	if ctx.Err() != nil {
		service.finishTerminated(
			ctx,
			campaign.Campaign.ID,
			taskID,
			campaign.AddressList.Entries,
		)
		return false
	}
	service.markRunning(taskID)
	if len(campaign.Documents) == 0 {
		return true
	}
	if _, err := service.converter.ResolveExecutable(); err != nil {
		diagnostic := safeDiagnostic(err.Error())
		service.recordRemaining(
			context.Background(),
			taskID,
			campaign.Campaign.ID,
			campaign.AddressList.Entries,
			"failed_processing",
			diagnostic,
		)
		service.finishCompleted(taskID, diagnostic)
		return false
	}
	return true
}

func (service *CampaignService) markRunning(taskID int64) {
	_ = service.updateTaskStatus(context.Background(), taskID, "running")
}

func (service *CampaignService) finishCompleted(taskID int64, lastError string) {
	ctx := context.Background()
	task, err := service.execution.GetTask(ctx, taskID)
	if err != nil {
		return
	}
	status := "completed"
	if task.Failed > 0 {
		status = "completed_with_errors"
	}
	_ = service.finishTask(ctx, taskID, status, safeDiagnostic(lastError))
}

func (service *CampaignService) finishTerminated(
	ctx context.Context,
	campaignID int64,
	taskID int64,
	remaining []store.AddressEntry,
) {
	status, message := termination(ctx)
	service.recordRemaining(context.Background(), taskID, campaignID, remaining, status, message)
	background := context.Background()
	_ = service.finishTask(background, taskID, status, message)
}

func (service *CampaignService) markDeliveryTerminated(deliveryID, taskID int64, ctx context.Context) {
	status, message := termination(ctx)
	background := context.Background()
	_ = service.execution.UpdateDeliveryStatus(background, deliveryID, status, "", message)
	_ = service.incrementTaskSkipped(background, taskID)
}

func (service *CampaignService) recordRemaining(
	ctx context.Context,
	taskID int64,
	campaignID int64,
	entries []store.AddressEntry,
	status string,
	message string,
) {
	for _, entry := range entries {
		_, err := service.execution.CreateDelivery(ctx, store.MessageDelivery{
			TaskID:         taskID,
			CampaignID:     optionalID(campaignID),
			AddressEntryID: optionalID(entry.ID),
			Email:          entry.Email,
			Status:         status,
			Attempt:        1,
			LastError:      safeDiagnostic(message),
		})
		if err != nil {
			continue
		}
		if strings.HasPrefix(status, "failed_") {
			_ = service.incrementTaskFailed(ctx, taskID)
		} else {
			_ = service.incrementTaskSkipped(ctx, taskID)
		}
	}
}

func (service *CampaignService) recordFailure(taskID, deliveryID int64, status string, err error) {
	service.recordFailureWithProvider(taskID, deliveryID, status, "", err)
}

func (service *CampaignService) recordFailureWithProvider(
	taskID int64,
	deliveryID int64,
	status string,
	providerID string,
	err error,
) {
	ctx := context.Background()
	if deliveryID > 0 {
		_ = service.execution.UpdateDeliveryStatus(ctx, deliveryID, status, providerID, safeDiagnostic(err.Error()))
	}
	_ = service.incrementTaskFailed(ctx, taskID)
}

func (service *CampaignService) updateTaskStatus(ctx context.Context, taskID int64, status string) error {
	err := service.execution.UpdateTaskStatus(ctx, taskID, status)
	if err == nil {
		service.notifyTask(taskID)
	}
	return err
}

func (service *CampaignService) finishTask(ctx context.Context, taskID int64, status, lastError string) error {
	err := service.execution.FinishTask(ctx, taskID, status, lastError)
	if err == nil {
		service.notifyTask(taskID)
	}
	return err
}

func (service *CampaignService) incrementTaskSent(ctx context.Context, taskID int64) error {
	err := service.execution.IncrementTaskSent(ctx, taskID)
	if err == nil {
		service.notifyTask(taskID)
	}
	return err
}

func (service *CampaignService) incrementTaskFailed(ctx context.Context, taskID int64) error {
	err := service.execution.IncrementTaskFailed(ctx, taskID)
	if err == nil {
		service.notifyTask(taskID)
	}
	return err
}

func (service *CampaignService) incrementTaskSkipped(ctx context.Context, taskID int64) error {
	err := service.execution.IncrementTaskSkipped(ctx, taskID)
	if err == nil {
		service.notifyTask(taskID)
	}
	return err
}

// Preview, placeholder, filename, and status helpers.

func messagePreview(
	entry store.AddressEntry,
	fields map[string]string,
	message mail.MessageContent,
) MessagePreview {
	preview := MessagePreview{
		AddressEntryID: entry.ID,
		Email:          entry.Email,
		Name:           personalizedName(entry, fields),
		Subject:        templates.RenderText(message.Subject, fields),
		Body:           appendTextFooter(templates.RenderText(message.Body, fields)),
	}
	if strings.TrimSpace(message.HTMLBody) != "" {
		preview.HTMLBody = appendHTMLFooter(templates.RenderHTML(message.HTMLBody, fields))
	}
	return preview
}

func sampleEntries(entries []store.AddressEntry, selectedID int64) []store.AddressEntry {
	indices := []int{0}
	if len(entries) > 1 {
		indices = append(indices, len(entries)-1)
	}
	if selectedID > 0 {
		for index, entry := range entries {
			if entry.ID == selectedID {
				indices = append(indices, index)
				break
			}
		}
	}
	seen := map[int]bool{}
	result := make([]store.AddressEntry, 0, len(indices))
	for _, index := range indices {
		if !seen[index] {
			seen[index] = true
			result = append(result, entries[index])
		}
	}
	return result
}

func neverPopulated(
	key string,
	entries []store.AddressEntry,
	options PersonalizationOptions,
) bool {
	for _, entry := range entries {
		if strings.TrimSpace(personalizedFields(entry, options)[key]) != "" {
			return false
		}
	}
	return true
}

func addLocations(target map[string]map[string]bool, location string, keys []string) {
	for _, key := range keys {
		if target[key] == nil {
			target[key] = map[string]bool{}
		}
		target[key][location] = true
	}
}

func sortedLocationKeys(locations map[string]map[string]bool) []string {
	keys := make([]string, 0, len(locations))
	for key := range locations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func mapKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func confirmationValue(issue UnresolvedPlaceholder) string {
	return issue.Key + ":" + issue.Reason
}

func confirmationMatches(expected, actual []string) bool {
	expected = append([]string(nil), expected...)
	actual = append([]string(nil), actual...)
	for index := range actual {
		actual[index] = strings.TrimSpace(actual[index])
	}
	sort.Strings(expected)
	sort.Strings(actual)
	if len(expected) != len(actual) {
		return false
	}
	for index := range expected {
		if expected[index] != actual[index] {
			return false
		}
	}
	return true
}

func termination(ctx context.Context) (string, string) {
	if errors.Is(context.Cause(ctx), tasks.ErrCancelled) {
		return "cancelled", tasks.ErrCancelled.Error()
	}
	return "interrupted", tasks.ErrInterrupted.Error()
}

func contextForStatus(ctx context.Context) context.Context {
	if ctx.Err() != nil {
		return context.Background()
	}
	return ctx
}

func optionalID(id int64) *int64 {
	if id <= 0 {
		return nil
	}
	return &id
}

func closeSender(sender mail.Sender) {
	if closer, ok := sender.(mail.CloseSender); ok {
		_ = closer.Close()
	}
}

func safeFilename(value string) string {
	name := filepath.Base(strings.TrimSpace(value))
	if name == "." || name == string(filepath.Separator) {
		return ""
	}
	return strings.NewReplacer("\\", "_", "/", "_", "\r", "_", "\n", "_").Replace(name)
}

func archiveDownloadName(campaignName string) string {
	name := document.SanitizeFilename(campaignName)
	name = strings.TrimSpace(strings.TrimSuffix(name, filepath.Ext(name)))
	if name == "" {
		name = "bulk-mail-documents"
	}
	return name + ".zip"
}

func campaignSizeError(actual, maximum int) error {
	return failure(
		ErrorValidation,
		fmt.Sprintf("campaign has %d address entries; the configured maximum is %d", actual, maximum),
		nil,
	)
}

func waitForRate(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func safeDiagnostic(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	characters := []rune(value)
	if len(characters) > 500 {
		value = string(characters[:500])
	}
	return value
}
