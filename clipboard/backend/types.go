package clipboard

import "time"

type CreatePasteRequest struct {
	Content       string `json:"content"`
	TTL           string `json:"ttl"`
	MaxViews      int    `json:"max_views,omitempty"`
	BurnAfterRead bool   `json:"burn_after_read,omitempty"`
}

type CreatePasteResponse struct {
	Code      string        `json:"code"`
	ShareURL  string        `json:"share_url"`
	RawURL    string        `json:"raw_url"`
	Paste     Paste         `json:"paste"`
	CreatedAt time.Time     `json:"created_at"`
	ExpiresAt time.Time     `json:"expires_at"`
	TTL       time.Duration `json:"ttl"`
}

type ReadPasteResponse struct {
	Paste Paste `json:"paste"`
}

type DeletePasteResponse struct {
	Code    string `json:"code"`
	Deleted bool   `json:"deleted"`
}

type Paste struct {
	Code     string        `json:"code"`
	Content  string        `json:"content"`
	Metadata PasteMetadata `json:"metadata"`
}

type PasteMetadata struct {
	TTL            string    `json:"ttl,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	BurnAfterRead  bool      `json:"burn_after_read,omitempty"`
	RemainingViews int       `json:"remaining_views,omitempty"`
}

type AdminPasteListItem struct {
	Code      string        `json:"code"`
	CreatedAt time.Time     `json:"created_at"`
	ExpiresAt time.Time     `json:"expires_at"`
	Metadata  PasteMetadata `json:"metadata"`
}

type AdminPasteListResponse struct {
	Items []AdminPasteListItem `json:"items"`
}

func (r CreatePasteRequest) ContentBytes() int {
	return len([]byte(r.Content))
}

func (r CreatePasteRequest) HasViewLimit() bool {
	return r.NormalizedMaxViews() > 0
}

func (r CreatePasteRequest) NormalizedMaxViews() int {
	if r.MaxViews <= 0 {
		return 0
	}

	return r.MaxViews
}

func (m PasteMetadata) HasRemainingViews() bool {
	return m.NormalizedRemainingViews() > 0
}

func (m PasteMetadata) NormalizedRemainingViews() int {
	if m.RemainingViews <= 0 {
		return 0
	}

	return m.RemainingViews
}
