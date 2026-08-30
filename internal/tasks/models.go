package tasks

// Persisted task models.

type Metadata struct {
	Mode                string   `json:"mode"`
	CampaignName        string   `json:"campaignName"`
	ProfileName         string   `json:"profileName,omitempty"`
	ListName            string   `json:"listName"`
	AttachmentNames     []string `json:"attachmentNames"`
	ConfirmedUnresolved []string `json:"confirmedUnresolved,omitempty"`
}

type Task struct {
	ID           int64    `json:"id"`
	CampaignID   *int64   `json:"campaignId"`
	CampaignName string   `json:"campaignName"`
	Metadata     Metadata `json:"-"`
	Status       string   `json:"status"`
	Total        int      `json:"total"`
	Sent         int      `json:"sent"`
	Failed       int      `json:"failed"`
	Skipped      int      `json:"skipped"`
	LastError    string   `json:"lastError"`
	CreatedAt    string   `json:"createdAt"`
	UpdatedAt    string   `json:"updatedAt"`
}

type Input struct {
	TaskID     int64
	ProfileID  int64
	StorageKey string
}
