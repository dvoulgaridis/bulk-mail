<script setup lang="ts">
import { useTemplateRef } from "vue";
import { useCampaignsFeature } from "../campaigns/useCampaigns";
import PersonalizationOptions from "../campaigns/PersonalizationOptions.vue";

const {
  campaign,
  handleAttachmentChange,
  removeAttachment,
  isDOCXAttachment,
  formatFileSize,
} = useCampaignsFeature();

const fileInput = useTemplateRef<HTMLInputElement>("fileInput");
</script>

<template>
  <fieldset class="bulk-mail-fieldset">
    <legend>Attachments</legend>
    <input
      ref="fileInput"
      class="visually-hidden"
      type="file"
      multiple
      @change="handleAttachmentChange"
    />
    <div class="app-stage-actions app-stage-actions--start">
      <button type="button" @click="fileInput?.click()">Browse</button>
    </div>
    <PersonalizationOptions :options="campaign.personalization.attachments">
      <label class="app-checkbox-field">
        <input v-model="campaign.personalization.attachments.convertDocxToPdf" type="checkbox" />
        <span>Convert DOCX to PDF</span>
      </label>
    </PersonalizationOptions>
    <div
      v-if="campaign.message.attachments.length > 0"
      class="bulk-mail-attachment-list"
    >
      <div
        v-for="(attachment, index) in campaign.message.attachments"
        :key="attachment.filename + index"
        class="bulk-mail-attachment-item"
      >
        <div>
          <strong>{{ attachment.filename }}</strong>
          <small>{{ formatFileSize(attachment.size) }}</small>
        </div>
        <label
          v-if="isDOCXAttachment(attachment) && campaign.personalization.attachments.convertDocxToPdf"
          class="app-form-field"
        >
          <span>Generated PDF filename</span>
          <input v-model="attachment.outputFilename" type="text" required />
        </label>
        <button
          type="button"
          class="bulk-mail-attachment-remove"
          @click="removeAttachment(index)"
        >
          Remove
        </button>
      </div>
    </div>
  </fieldset>
</template>
