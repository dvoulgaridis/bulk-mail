<script setup lang="ts">
import { useCampaignsFeature } from "../campaigns/useCampaigns";

const {
  campaign,
  openAttachmentPicker,
  handleAttachmentChange,
  removeAttachment,
  isDOCXAttachment,
  formatFileSize,
} = useCampaignsFeature();
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
      <button type="button" @click="openAttachmentPicker">Browse</button>
    </div>
    <p class="app-form-help">
      * DOCX files support placeholders and are converted to PDF; other files
      are attached unchanged
    </p>
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
        <label v-if="isDOCXAttachment(attachment)" class="app-form-field">
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
