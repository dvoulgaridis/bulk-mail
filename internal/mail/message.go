package mail

type MessageContent struct {
	Subject               string       `json:"subject"`
	Body                  string       `json:"body"`
	HTMLBody              string       `json:"htmlBody"`
	RequestDeliveryNotice bool         `json:"requestDeliveryNotice"`
	Attachments           []Attachment `json:"attachments"`
}

type Message struct {
	ToEmail string `json:"-"`
	ToName  string `json:"-"`
	MessageContent
}

type Attachment struct {
	Filename       string `json:"filename"`
	OutputFilename string `json:"outputFilename"`
	ContentType    string `json:"contentType,omitempty"`
	Size           int    `json:"size"`
	Content        []byte `json:"contentBase64,omitempty"`
}
