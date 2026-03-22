package clipboard

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	redis "github.com/redis/go-redis/v9"
)

var ErrPasteNotFound = errors.New("paste not found")

type PasteServiceOptions struct {
	BaseURL           string
	ShortcodeReader   io.Reader
	Now               func() time.Time
	ShortcodeAttempts int
}

type PasteService struct {
	client            redis.UniversalClient
	baseURL           string
	shortcodeReader   io.Reader
	now               func() time.Time
	shortcodeAttempts int
	stats             serviceStats
}

type serviceStats struct {
	pastesCreatedTotal     atomic.Int64
	pastesReadTotal        atomic.Int64
	pastesExpiredTotal     atomic.Int64
	pastesBurnedTotal      atomic.Int64
	pastesViewLimitedTotal atomic.Int64
}

func NewPasteService(client redis.UniversalClient, options PasteServiceOptions) *PasteService {
	reader := options.ShortcodeReader
	if reader == nil {
		reader = rand.Reader
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}

	attempts := options.ShortcodeAttempts
	if attempts <= 0 {
		attempts = 5
	}

	return &PasteService{
		client:            client,
		baseURL:           strings.TrimRight(options.BaseURL, "/"),
		shortcodeReader:   reader,
		now:               now,
		shortcodeAttempts: attempts,
	}
}

func (s *PasteService) Create(ctx context.Context, request CreatePasteRequest) (CreatePasteResponse, error) {
	if err := ValidateCreateRequest(request); err != nil {
		return CreatePasteResponse{}, err
	}

	ttl, err := ttlDuration(request.TTL)
	if err != nil {
		return CreatePasteResponse{}, err
	}

	code, err := GenerateUniqueShortcode(ctx, s.shortcodeReader, func(ctx context.Context, candidate string) (bool, error) {
		count, err := s.client.Exists(ctx, pasteKey(candidate)).Result()
		if err != nil {
			return false, fmt.Errorf("check paste key exists: %w", err)
		}
		return count > 0, nil
	}, s.shortcodeAttempts)
	if err != nil {
		return CreatePasteResponse{}, err
	}

	createdAt := s.now().UTC()
	expiresAt := createdAt.Add(ttl)
	fields := map[string]any{
		"content":         request.Content,
		"ttl":             request.TTL,
		"created_at":      createdAt.Format(time.RFC3339Nano),
		"expires_at":      expiresAt.Format(time.RFC3339Nano),
		"burn_after_read": strconv.FormatBool(request.BurnAfterRead),
	}

	if err := s.client.HSet(ctx, pasteKey(code), fields).Err(); err != nil {
		return CreatePasteResponse{}, fmt.Errorf("store paste metadata: %w", err)
	}
	if err := s.client.Expire(ctx, pasteKey(code), ttl).Err(); err != nil {
		return CreatePasteResponse{}, fmt.Errorf("expire paste metadata: %w", err)
	}
	if request.HasViewLimit() {
		if err := s.client.Set(ctx, viewsKey(code), request.NormalizedMaxViews(), ttl).Err(); err != nil {
			return CreatePasteResponse{}, fmt.Errorf("store paste view limit: %w", err)
		}
	}

	s.stats.pastesCreatedTotal.Add(1)
	recordNetcutAccess("paste_create")

	paste := Paste{
		Code:    code,
		Content: request.Content,
		Metadata: PasteMetadata{
			TTL:            request.TTL,
			CreatedAt:      createdAt,
			ExpiresAt:      expiresAt,
			BurnAfterRead:  request.BurnAfterRead,
			RemainingViews: request.NormalizedMaxViews(),
		},
	}

	return CreatePasteResponse{
		Code:      code,
		ShareURL:  buildPasteURL(s.baseURL, code),
		RawURL:    buildRawURL(s.baseURL, code),
		Paste:     paste,
		CreatedAt: createdAt,
		ExpiresAt: expiresAt,
		TTL:       ttl,
	}, nil
}

func (s *PasteService) Get(ctx context.Context, code string) (ReadPasteResponse, error) {
	paste, hasViewCounter, err := s.lookupPaste(ctx, code)
	if err != nil {
		if errors.Is(err, ErrPasteNotFound) {
			s.stats.pastesExpiredTotal.Add(1)
		}
		return ReadPasteResponse{}, err
	}

	if paste.Metadata.BurnAfterRead {
		claimResult, err := s.client.Do(ctx, "SETNX", burnClaimKey(code), "1").Int()
		if err != nil {
			return ReadPasteResponse{}, fmt.Errorf("claim burn-after-read paste: %w", err)
		}
		if claimResult != 1 {
			s.stats.pastesBurnedTotal.Add(1)
			return ReadPasteResponse{}, ErrPasteNotFound
		}
	}

	if hasViewCounter && paste.Metadata.RemainingViews <= 0 {
		s.stats.pastesViewLimitedTotal.Add(1)
		return ReadPasteResponse{}, ErrPasteNotFound
	}

	if paste.Metadata.RemainingViews > 0 {
		newRemaining, err := s.client.Decr(ctx, viewsKey(code)).Result()
		if err != nil {
			return ReadPasteResponse{}, fmt.Errorf("decrement paste view limit: %w", err)
		}
		if newRemaining < 0 {
			s.stats.pastesViewLimitedTotal.Add(1)
			return ReadPasteResponse{}, ErrPasteNotFound
		}
		paste.Metadata.RemainingViews = int(newRemaining + 1)
	}

	if paste.Metadata.BurnAfterRead {
		if _, err := s.client.Del(ctx, pasteKey(code), viewsKey(code)).Result(); err != nil {
			return ReadPasteResponse{}, fmt.Errorf("delete burned paste: %w", err)
		}
		s.stats.pastesBurnedTotal.Add(1)
	}

	s.stats.pastesReadTotal.Add(1)
	recordNetcutAccess("paste_read")

	return ReadPasteResponse{Paste: paste}, nil
}

func (s *PasteService) Delete(ctx context.Context, code string) (DeletePasteResponse, error) {
	deleted, err := s.client.Del(ctx, pasteKey(code), viewsKey(code), burnClaimKey(code)).Result()
	if err != nil {
		return DeletePasteResponse{}, fmt.Errorf("delete paste: %w", err)
	}

	return DeletePasteResponse{Code: code, Deleted: deleted > 0}, nil
}

func (s *PasteService) ListAdmin(ctx context.Context) (AdminPasteListResponse, error) {
	keys, err := s.client.Keys(ctx, "paste:*").Result()
	if err != nil {
		return AdminPasteListResponse{}, fmt.Errorf("list paste keys: %w", err)
	}

	items := make([]AdminPasteListItem, 0, len(keys))
	for _, key := range keys {
		if strings.HasSuffix(key, ":views_remaining") || strings.HasSuffix(key, ":burn_claim") {
			continue
		}

		code := strings.TrimPrefix(key, "paste:")
		paste, hasViewCounter, err := s.lookupPaste(ctx, code)
		if err != nil {
			if errors.Is(err, ErrPasteNotFound) {
				continue
			}
			return AdminPasteListResponse{}, err
		}
		if hasViewCounter && paste.Metadata.RemainingViews <= 0 {
			continue
		}

		items = append(items, AdminPasteListItem{
			Code:      paste.Code,
			CreatedAt: paste.Metadata.CreatedAt,
			ExpiresAt: paste.Metadata.ExpiresAt,
			Metadata:  paste.Metadata,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})

	return AdminPasteListResponse{Items: items}, nil
}

func (s *PasteService) Stats() UsageStatsDTO {
	return UsageStatsDTO{
		PastesCreatedTotal:     s.stats.pastesCreatedTotal.Load(),
		PastesReadTotal:        s.stats.pastesReadTotal.Load(),
		PastesExpiredTotal:     s.stats.pastesExpiredTotal.Load(),
		PastesBurnedTotal:      s.stats.pastesBurnedTotal.Load(),
		PastesViewLimitedTotal: s.stats.pastesViewLimitedTotal.Load(),
	}
}

func pasteKey(code string) string {
	return "paste:" + code
}

func viewsKey(code string) string {
	return pasteKey(code) + ":views_remaining"
}

func burnClaimKey(code string) string {
	return pasteKey(code) + ":burn_claim"
}

func buildPasteURL(baseURL, code string) string {
	if baseURL == "" {
		return "/p/" + code
	}

	return baseURL + "/p/" + code
}

func buildRawURL(baseURL, code string) string {
	if baseURL == "" {
		return "/raw/" + code
	}

	return baseURL + "/raw/" + code
}

func ttlDuration(ttl string) (time.Duration, error) {
	parsed, err := time.ParseDuration(ttl)
	if err == nil {
		return parsed, nil
	}
	if ttl == "1d" {
		return 24 * time.Hour, nil
	}
	if ttl == "7d" {
		return 7 * 24 * time.Hour, nil
	}

	return 0, ErrInvalidTTL
}

func metadataFromFields(fields map[string]string) (PasteMetadata, error) {
	createdAt, err := time.Parse(time.RFC3339Nano, fields["created_at"])
	if err != nil {
		return PasteMetadata{}, fmt.Errorf("parse created_at: %w", err)
	}

	expiresAt, err := time.Parse(time.RFC3339Nano, fields["expires_at"])
	if err != nil {
		return PasteMetadata{}, fmt.Errorf("parse expires_at: %w", err)
	}

	burnAfterRead, err := strconv.ParseBool(fields["burn_after_read"])
	if err != nil {
		return PasteMetadata{}, fmt.Errorf("parse burn_after_read: %w", err)
	}

	return PasteMetadata{
		TTL:           fields["ttl"],
		CreatedAt:     createdAt,
		ExpiresAt:     expiresAt,
		BurnAfterRead: burnAfterRead,
	}, nil
}

func (s *PasteService) lookupPaste(ctx context.Context, code string) (Paste, bool, error) {
	fields, err := s.client.HGetAll(ctx, pasteKey(code)).Result()
	if err != nil {
		return Paste{}, false, fmt.Errorf("get paste metadata: %w", err)
	}
	if len(fields) == 0 {
		return Paste{}, false, ErrPasteNotFound
	}

	metadata, err := metadataFromFields(fields)
	if err != nil {
		return Paste{}, false, err
	}

	remainingViews, err := s.client.Get(ctx, viewsKey(code)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return Paste{}, false, fmt.Errorf("get paste view limit: %w", err)
	}
	hasViewCounter := err == nil
	if err == nil {
		metadata.RemainingViews, err = strconv.Atoi(remainingViews)
		if err != nil {
			return Paste{}, false, fmt.Errorf("parse remaining views: %w", err)
		}
	}

	return Paste{
		Code:     code,
		Content:  fields["content"],
		Metadata: metadata,
	}, hasViewCounter, nil
}
