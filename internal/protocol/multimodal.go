package protocol

type MessagePart struct {
	Type   string        `json:"type"`
	Text   string        `json:"text,omitempty"`
	Source AttachmentRef `json:"source,omitempty"`
}

// AttachmentRef 描述媒体来源；本地客户端通常用 path/base64，Web 客户端后续应优先用 blob_id。
type AttachmentRef struct {
	Kind     string `json:"kind"`
	Path     string `json:"path,omitempty"`
	URL      string `json:"url,omitempty"`
	Base64   string `json:"base64,omitempty"`
	BlobID   string `json:"blob_id,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	Name     string `json:"name,omitempty"`
	Size     int64  `json:"size,omitempty"`
}
