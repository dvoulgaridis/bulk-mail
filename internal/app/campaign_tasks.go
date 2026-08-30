package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"strings"

	"github.com/dvoulgaridis/bulk-mail/internal/mail"
	"github.com/dvoulgaridis/bulk-mail/internal/store"
	"github.com/dvoulgaridis/bulk-mail/internal/tasks"
	"github.com/dvoulgaridis/bulk-mail/internal/validation"
)

const taskProfileFilename = "profile.json"

type CampaignTaskSnapshot struct {
	Campaign    store.Campaign    `json:"campaign"`
	Settings    store.AppSettings `json:"settings"`
	AddressList store.AddressList `json:"addressList"`
}

// Task submission.

func (service *CampaignService) QueueCampaign(
	ctx context.Context,
	command ExecuteCampaignCommand,
) (tasks.Task, error) {
	campaign, err := service.loadSavedCampaign(ctx, command.CampaignID)
	if err != nil {
		return tasks.Task{}, err
	}
	if campaign.ProfileID == nil {
		return tasks.Task{}, failure(ErrorValidation, "profile is required", nil)
	}
	if strings.TrimSpace(campaign.Message.Subject) == "" {
		return tasks.Task{}, failure(ErrorValidation, "subject is required", nil)
	}
	if strings.TrimSpace(campaign.Message.Body) == "" {
		return tasks.Task{}, failure(ErrorValidation, "message body is required", nil)
	}
	snapshot, err := service.captureCampaignSnapshot(ctx, campaign)
	if err != nil {
		return tasks.Task{}, err
	}
	profile, err := service.delivery.Profile(ctx, *snapshot.Campaign.ProfileID)
	if err != nil {
		return tasks.Task{}, err
	}
	snapshot, files, names, err := extractTaskAttachmentFiles(snapshot)
	if err != nil {
		return tasks.Task{}, err
	}
	profileFile, err := encodeTaskJSONFile(taskProfileFilename, profile)
	if err != nil {
		return tasks.Task{}, internalFailure("encode sender profile task file", err)
	}
	files = append(files, profileFile)
	return service.submitCampaignTask(ctx, snapshot, files, tasks.Metadata{
		Mode:                "send",
		CampaignName:        snapshot.Campaign.Name,
		ProfileName:         profile.Name,
		ListName:            snapshot.AddressList.Name,
		AttachmentNames:     names,
		ConfirmedUnresolved: command.ConfirmedUnresolved,
	})
}

func (service *CampaignService) QueueDocumentGeneration(
	ctx context.Context,
	command ExecuteCampaignCommand,
) (tasks.Task, error) {
	campaign, err := service.loadSavedCampaign(ctx, command.CampaignID)
	if err != nil {
		return tasks.Task{}, err
	}
	snapshot, err := service.captureCampaignSnapshot(ctx, campaign)
	if err != nil {
		return tasks.Task{}, err
	}
	snapshot, files, names, err := extractTaskAttachmentFiles(snapshot)
	if err != nil {
		return tasks.Task{}, err
	}
	hasDocument := false
	for _, attachment := range snapshot.Campaign.Message.Attachments {
		if isDOCXFilename(attachment.Filename) {
			hasDocument = true
			break
		}
	}
	if !hasDocument {
		return tasks.Task{}, failure(ErrorValidation, "choose at least one DOCX document", nil)
	}
	return service.submitCampaignTask(ctx, snapshot, files, tasks.Metadata{
		Mode:                "generate",
		CampaignName:        snapshot.Campaign.Name,
		ListName:            snapshot.AddressList.Name,
		AttachmentNames:     names,
		ConfirmedUnresolved: command.ConfirmedUnresolved,
	})
}

// Campaign task snapshot construction.

func (service *CampaignService) loadSavedCampaign(ctx context.Context, id int64) (store.Campaign, error) {
	if id <= 0 {
		return store.Campaign{}, failure(ErrorValidation, "save the campaign before queueing it", nil)
	}
	campaign, err := service.execution.GetCampaign(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Campaign{}, failure(ErrorNotFound, "campaign not found", err)
	}
	if err != nil {
		return store.Campaign{}, internalFailure("load campaign", err)
	}
	return campaign, nil
}

func (service *CampaignService) captureCampaignSnapshot(
	ctx context.Context,
	campaign store.Campaign,
) (CampaignTaskSnapshot, error) {
	settings, err := service.settings.GetAppSettings(ctx)
	if err != nil {
		return CampaignTaskSnapshot{}, internalFailure("load application settings", err)
	}
	list, err := service.loadAddressList(ctx, campaign.AddressListID)
	if err != nil {
		return CampaignTaskSnapshot{}, err
	}
	snapshot := CampaignTaskSnapshot{
		Campaign:    campaign,
		Settings:    settings,
		AddressList: list,
	}
	if err := validateCampaignRuntime(snapshot); err != nil {
		return CampaignTaskSnapshot{}, err
	}
	snapshot.AddressList.Fields = append(
		[]store.AddressFieldDefinition(nil),
		snapshot.AddressList.Fields...,
	)
	snapshot.AddressList.Entries = append(
		[]store.AddressEntry(nil),
		snapshot.AddressList.Entries...,
	)
	for index := range snapshot.AddressList.Entries {
		entry := &snapshot.AddressList.Entries[index]
		entry.ID = 0
		entry.Fields = maps.Clone(entry.Fields)
	}
	return snapshot, nil
}

func extractTaskAttachmentFiles(
	snapshot CampaignTaskSnapshot,
) (CampaignTaskSnapshot, []tasks.StoredFile, []string, error) {
	if err := validateAttachmentCount(
		len(snapshot.Campaign.Message.Attachments),
		snapshot.Settings.MaxCampaignDocuments,
	); err != nil {
		return CampaignTaskSnapshot{}, nil, nil, err
	}
	snapshot.Campaign.Message.Attachments = append(
		[]mail.Attachment(nil),
		snapshot.Campaign.Message.Attachments...,
	)
	files := make([]tasks.StoredFile, 0, len(snapshot.Campaign.Message.Attachments))
	names := make([]string, 0, len(snapshot.Campaign.Message.Attachments))
	for index := range snapshot.Campaign.Message.Attachments {
		input := &snapshot.Campaign.Message.Attachments[index]
		filename, err := validateAttachmentSource(*input)
		if err != nil {
			return CampaignTaskSnapshot{}, nil, nil, err
		}
		files = append(files, tasks.StoredFile{
			Name: storedAttachmentFilename(index),
			Data: input.Content,
		})
		input.Filename = filename
		input.Size = len(input.Content)
		input.Content = nil
		names = append(names, filename)
	}
	return snapshot, files, names, nil
}

func storedAttachmentFilename(index int) string {
	return fmt.Sprintf("attachment-%06d", index)
}

func encodeTaskJSONFile(name string, value any) (tasks.StoredFile, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return tasks.StoredFile{}, err
	}
	return tasks.StoredFile{Name: name, Data: data}, nil
}

func (service *CampaignService) submitCampaignTask(
	ctx context.Context,
	snapshot CampaignTaskSnapshot,
	files []tasks.StoredFile,
	metadata tasks.Metadata,
) (tasks.Task, error) {
	manifestJSON, err := json.Marshal(snapshot)
	if err != nil {
		return tasks.Task{}, internalFailure("encode campaign task snapshot", err)
	}
	profileID := int64(0)
	if metadata.Mode == "send" && snapshot.Campaign.ProfileID != nil {
		profileID = *snapshot.Campaign.ProfileID
	}
	task, err := service.taskQueue.Submit(ctx, tasks.Submission{
		CampaignID: snapshot.Campaign.ID,
		ProfileID:  profileID,
		Total:      len(snapshot.AddressList.Entries),
		Metadata:   metadata,
		Manifest:   manifestJSON,
		Files:      files,
	})
	if err != nil {
		if errors.Is(err, tasks.ErrQueueFull) {
			return tasks.Task{}, failure(
				ErrorCapacity,
				"the task queue is full; wait for a queued or running campaign to finish",
				err,
			)
		}
		return tasks.Task{}, internalFailure("queue campaign task", err)
	}
	return task, nil
}

// Task execution.

func (service *CampaignService) ExecuteCampaignTask(ctx context.Context, taskID int64) {
	task, err := service.execution.GetTask(contextForStatus(ctx), taskID)
	if err != nil {
		slog.Error("load claimed task", "task_id", taskID, "error", err)
		return
	}
	campaignID := int64(0)
	if task.CampaignID != nil {
		campaignID = *task.CampaignID
	}
	snapshot, profile, err := service.loadCampaignTaskSnapshot(ctx, taskID, task.Metadata.Mode)
	if err != nil {
		service.finishPreparationFailure(
			ctx,
			campaignID,
			taskID,
			snapshot.AddressList.Entries,
			err,
		)
		return
	}
	campaign, validation, err := validateCampaignTaskSnapshot(snapshot, task.Metadata.Mode)
	if err == nil && !confirmationMatches(
		validation.Confirmation,
		task.Metadata.ConfirmedUnresolved,
	) {
		err = errors.New("confirmed unresolved placeholders no longer match campaign inputs")
	}
	if err != nil {
		service.finishPreparationFailure(
			ctx,
			campaignID,
			taskID,
			campaign.AddressList.Entries,
			err,
		)
		return
	}
	if ctx.Err() != nil {
		service.finishTerminated(ctx, campaignID, taskID, campaign.AddressList.Entries)
		return
	}
	switch task.Metadata.Mode {
	case "generate":
		service.executeGeneration(ctx, preparedGeneration{
			taskID:   taskID,
			campaign: campaign,
		})
	case "send":
		if profile == nil {
			service.finishPreparationFailure(
				ctx,
				campaignID,
				taskID,
				campaign.AddressList.Entries,
				errors.New("campaign task has no sender profile"),
			)
			return
		}
		sender, err := service.delivery.SenderForProfile(ctx, *profile)
		if err != nil {
			service.finishPreparationFailure(
				ctx,
				campaignID,
				taskID,
				campaign.AddressList.Entries,
				err,
			)
			return
		}
		service.executeSend(ctx, preparedSend{
			taskID:   taskID,
			sender:   sender,
			campaign: campaign,
		})
	default:
		service.finishPreparationFailure(
			ctx,
			campaignID,
			taskID,
			campaign.AddressList.Entries,
			fmt.Errorf("unsupported campaign task mode %q", task.Metadata.Mode),
		)
	}
}

func (service *CampaignService) RecoverInterruptedCampaignTask(
	ctx context.Context,
	taskID int64,
	manifest []byte,
	inputErr error,
) error {
	const interrupted = "application stopped before task completion"
	diagnostic := interrupted
	var emails []string
	if inputErr != nil {
		diagnostic = safeDiagnostic(fmt.Sprintf("%s; task input is unavailable: %v", interrupted, inputErr))
	} else {
		var snapshot CampaignTaskSnapshot
		if err := decodeTaskJSON(manifest, &snapshot); err != nil {
			diagnostic = safeDiagnostic(fmt.Sprintf("%s; task manifest is invalid: %v", interrupted, err))
		} else if values, err := normalizedSnapshotEmails(snapshot); err != nil {
			diagnostic = safeDiagnostic(fmt.Sprintf("%s; task manifest is invalid: %v", interrupted, err))
		} else {
			emails = values
		}
	}
	return service.execution.FinalizeInterruptedTask(ctx, taskID, emails, diagnostic)
}

func (service *CampaignService) CancelQueuedCampaignTask(
	ctx context.Context,
	taskID int64,
	manifest []byte,
) (bool, error) {
	var snapshot CampaignTaskSnapshot
	if err := decodeTaskJSON(manifest, &snapshot); err != nil {
		return false, fmt.Errorf("decode queued campaign task snapshot: %w", err)
	}
	emails, err := normalizedSnapshotEmails(snapshot)
	if err != nil {
		return false, fmt.Errorf("validate queued campaign task snapshot: %w", err)
	}
	return service.execution.CancelQueuedCampaignTask(ctx, taskID, emails)
}

func normalizedSnapshotEmails(snapshot CampaignTaskSnapshot) ([]string, error) {
	emails := make([]string, 0, len(snapshot.AddressList.Entries))
	for _, entry := range snapshot.AddressList.Entries {
		email, err := validation.NormalizeEmail(entry.Email)
		if err != nil {
			return nil, err
		}
		emails = append(emails, email)
	}
	return emails, nil
}

func (service *CampaignService) loadCampaignTaskSnapshot(
	ctx context.Context,
	taskID int64,
	mode string,
) (CampaignTaskSnapshot, *store.SMTPProfile, error) {
	payload, err := service.taskQueue.LoadPayload(contextForStatus(ctx), taskID)
	if err != nil {
		return CampaignTaskSnapshot{}, nil, fmt.Errorf("load campaign task input: %w", err)
	}
	var snapshot CampaignTaskSnapshot
	if err := decodeTaskJSON(payload.Manifest(), &snapshot); err != nil {
		return CampaignTaskSnapshot{}, nil, fmt.Errorf("decode campaign task snapshot: %w", err)
	}
	var profile *store.SMTPProfile
	if mode == "send" {
		profile = &store.SMTPProfile{}
		if err := loadTaskJSONFile(payload, taskProfileFilename, profile); err != nil {
			return snapshot, nil, err
		}
	}
	for index := range snapshot.Campaign.Message.Attachments {
		if err := ctx.Err(); err != nil {
			return snapshot, profile, err
		}
		content, err := payload.ReadFile(storedAttachmentFilename(index))
		if err != nil {
			return snapshot, profile, err
		}
		snapshot.Campaign.Message.Attachments[index].Content = content
	}
	return snapshot, profile, nil
}

func loadTaskJSONFile(payload tasks.Payload, name string, target any) error {
	data, err := payload.ReadFile(name)
	if err != nil {
		return err
	}
	if err := decodeTaskJSON(data, target); err != nil {
		return fmt.Errorf("decode task file %s: %w", name, err)
	}
	return nil
}

func decodeTaskJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("task JSON has trailing data")
	}
	return nil
}

func validateCampaignTaskSnapshot(
	snapshot CampaignTaskSnapshot,
	mode string,
) (preparedCampaign, PreflightResult, error) {
	if mode != "send" && mode != "generate" {
		return preparedCampaign{}, PreflightResult{}, fmt.Errorf(
			"unsupported campaign task mode %q",
			mode,
		)
	}
	campaign, err := prepareCampaign(snapshot)
	if err != nil {
		return preparedCampaign{}, PreflightResult{}, err
	}
	return validatePreparedCampaign(campaign, mode)
}

// Task cancellation and preparation failures.

func (service *CampaignService) CancelTask(ctx context.Context, taskID int64) (bool, error) {
	return service.taskQueue.Cancel(ctx, taskID)
}

func (service *CampaignService) finishPreparationFailure(
	ctx context.Context,
	campaignID int64,
	taskID int64,
	entries []store.AddressEntry,
	err error,
) {
	if ctx.Err() != nil {
		service.finishTerminated(ctx, campaignID, taskID, entries)
		return
	}
	diagnostic := safeDiagnostic(err.Error())
	if len(entries) > 0 {
		service.recordRemaining(
			context.Background(),
			taskID,
			campaignID,
			entries,
			"failed_processing",
			diagnostic,
		)
	}
	background := context.Background()
	_ = service.finishTask(background, taskID, "completed_with_errors", diagnostic)
}
