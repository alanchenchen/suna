package protocol

type MessagePart struct {
	Type   string        `json:"type"`
	Text   string        `json:"text,omitempty"`
	Source AttachmentRef `json:"source,omitempty"`
}

// AttachmentRef 描述媒体来源。对外协议只接受 path/url；base64 只允许作为 daemon 内部到 provider 的短生命周期格式。
type AttachmentRef struct {
	Kind     string `json:"kind"` // path | url
	Path     string `json:"path,omitempty"`
	URL      string `json:"url,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	Name     string `json:"name,omitempty"`
	Size     int64  `json:"size,omitempty"`
}
