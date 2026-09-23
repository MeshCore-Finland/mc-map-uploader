package uploader

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"mc-map-uploader/internal/mqtt"
)

type Config struct {
	APIURL           string
	KeyFile          string
	AllowedIATA      string
	DryRun           bool
	HTTPTimeout      time.Duration
	QueueSize        int
	Workers          int
	Attempts         int
	RetryDelay       time.Duration
	GeofencePolygons []Polygon
}

type statusEntry struct {
	radio   Radio
	expires time.Time
}

type nodeEntry struct {
	handled uint32
	expires time.Time
	pending bool
}

type Job struct {
	Advert Advert
	Radio  Radio
}

type result struct {
	job     Job
	handled bool
	err     error
}

// Service owns all state inside the uploader process. Handle and Run must be
// called from the same goroutine; the HTTP worker communicates via results.
type Service struct {
	cfg              Config
	logger           *slog.Logger
	status           map[string]statusEntry
	nodes            map[string]nodeEntry
	dedup            map[string]time.Time
	jobs             chan Job
	results          chan result
	poster           *Poster
	allowed          map[string]struct{}
	geofence         geofence
	nextProcessingID uint64
}

var iataPattern = regexp.MustCompile(`^[A-Z]+$`)

func parseAllowlist(raw string) (map[string]struct{}, error) {
	allowed := make(map[string]struct{})
	for _, item := range strings.Split(raw, ",") {
		code := strings.TrimSpace(item)
		if !iataPattern.MatchString(code) {
			return nil, errors.New("map.allowed_iata must be a nonempty list of uppercase letters-only codes")
		}
		allowed[code] = struct{}{}
	}
	return allowed, nil
}

// ParseAllowlist validates the codes used in the YAML config.
func ParseAllowlist(codes []string) (map[string]struct{}, error) {
	return parseAllowlist(strings.Join(codes, ","))
}

func New(cfg Config, logger *slog.Logger) (*Service, error) {
	if cfg.Workers == 0 {
		cfg.Workers = 1
	}
	if cfg.APIURL == "" || cfg.HTTPTimeout <= 0 || cfg.QueueSize < 1 || cfg.QueueSize > 10000 || cfg.Workers < 1 || cfg.Workers > 20 || cfg.Attempts < 1 || cfg.Attempts > 20 {
		return nil, errors.New("invalid uploader configuration")
	}
	allowed, err := parseAllowlist(cfg.AllowedIATA)
	if err != nil {
		return nil, err
	}
	fence, err := newGeofence(cfg.GeofencePolygons)
	if err != nil {
		return nil, err
	}
	seed, err := ReadKeyFile(cfg.KeyFile)
	if err != nil {
		return nil, err
	}
	poster, err := NewPoster(cfg.APIURL, cfg.HTTPTimeout, cfg.DryRun, seed, logger)
	if err != nil {
		return nil, err
	}
	return &Service{cfg: cfg, logger: logger, status: make(map[string]statusEntry), nodes: make(map[string]nodeEntry), dedup: make(map[string]time.Time), jobs: make(chan Job, cfg.QueueSize), results: make(chan result, 1), poster: poster, allowed: allowed, geofence: fence}, nil
}

func (s *Service) Handle(msg mqtt.Message) {
	observer, kind, ok := topicParts(msg.Topic)
	if !ok || len(msg.Payload) > 16*1024 {
		return
	}
	iata := strings.Split(msg.Topic, "/")[1]
	now := time.Now()
	if kind == "status" {
		if _, ok := s.allowed[iata]; !ok {
			return
		}
		radio, err := ParseRadio(msg.Payload)
		if err == nil {
			s.status[observer] = statusEntry{radio, now.Add(24 * time.Hour)}
			if len(s.status) > 10000 {
				s.sweep(now)
			}
		}
		return
	}
	bytes, err := ParsePacket(msg.Payload)
	if err != nil {
		return
	}
	if len(bytes) == 0 || (bytes[0]>>2)&0x0f != 4 {
		return
	}
	advert, err := DecodeAdvert(bytes)
	s.nextProcessingID++
	processingID := fmt.Sprintf("%d-%d", now.UnixNano(), s.nextProcessingID)
	attrs := advertLogFields(processingID, advert)
	s.logger.Info("advert received", append(attrs, "region", iata, "observer", observer)...)
	if err != nil {
		s.logAdvertComplete(processingID, advert, "dropped", err.Error())
		return
	}
	if _, ok := s.allowed[iata]; !ok {
		s.logAdvertComplete(processingID, advert, "dropped", "region_not_allowed")
		return
	}
	entry, ok := s.status[observer]
	if !ok || !entry.expires.After(now) {
		s.logAdvertComplete(processingID, advert, "dropped", "observer_status_unavailable")
		return
	}
	if !s.geofence.contains(advert.Latitude, advert.Longitude) {
		s.logAdvertComplete(processingID, advert, "dropped", "outside_geofence")
		return
	}
	if until, seen := s.dedup[advert.PacketID]; seen && until.After(now) {
		s.logAdvertComplete(processingID, advert, "dropped", "packet_duplicate")
		return
	}
	s.dedup[advert.PacketID] = now.Add(time.Minute)
	state := s.nodes[advert.PublicKey]
	if state.pending {
		s.logAdvertComplete(processingID, advert, "dropped", "node_pending")
		return
	}
	if state.expires.After(now) && (advert.Timestamp <= state.handled || uint64(advert.Timestamp) < uint64(state.handled)+3600) {
		s.logAdvertComplete(processingID, advert, "dropped", "timestamp_too_soon")
		return
	}
	job := Job{Advert: advert, Radio: entry.radio}
	select {
	case s.jobs <- job:
		state.pending = true
		s.nodes[advert.PublicKey] = state
		s.logAdvertComplete(processingID, advert, "queued", "upload_queued")
	default:
		s.logAdvertComplete(processingID, advert, "dropped", "upload_queue_full")
	}
	if len(s.dedup) > 50000 || len(s.nodes) > 50000 {
		s.sweep(now)
	}
}

func (s *Service) logAdvertComplete(processingID string, advert Advert, outcome, reason string) {
	s.logger.Info("advert processing complete", append(advertLogFields(processingID, advert), "outcome", outcome, "reason", reason)...)
}

func advertLogFields(processingID string, advert Advert) []any {
	attrs := []any{"processing_id", processingID, "packet_id", advert.PacketID, "node", advert.PublicKey}
	if advert.Name != "" {
		attrs = append(attrs, "name", advert.Name)
	}
	if advert.Type != "" {
		attrs = append(attrs, "type", advert.Type)
	}
	return attrs
}

func (s *Service) sweep(now time.Time) {
	for key, state := range s.status {
		if !state.expires.After(now) {
			delete(s.status, key)
		}
	}
	for key, state := range s.nodes {
		if !state.pending && !state.expires.After(now) {
			delete(s.nodes, key)
		}
	}
	for key, until := range s.dedup {
		if !until.After(now) {
			delete(s.dedup, key)
		}
	}
	// Hard caps still apply if an extreme burst arrives within one TTL.
	for len(s.status) > 10000 {
		for key := range s.status {
			delete(s.status, key)
			break
		}
	}
	for len(s.dedup) > 50000 {
		for key := range s.dedup {
			delete(s.dedup, key)
			break
		}
	}
	for len(s.nodes) > 50000 {
		removed := false
		for key, state := range s.nodes {
			if !state.pending {
				delete(s.nodes, key)
				removed = true
				break
			}
		}
		if !removed { // Pending jobs must remain until their results arrive.
			break
		}
	}
}

func (s *Service) Run(ctx context.Context, messages <-chan mqtt.Message) {
	for range s.cfg.Workers {
		go s.worker(ctx)
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-messages:
			s.Handle(msg)
		case outcome := <-s.results:
			state := s.nodes[outcome.job.Advert.PublicKey]
			state.pending = false
			if outcome.handled {
				state.handled = outcome.job.Advert.Timestamp
				state.expires = time.Now().Add(72 * time.Hour)
				s.nodes[outcome.job.Advert.PublicKey] = state
			} else if state.expires.IsZero() {
				delete(s.nodes, outcome.job.Advert.PublicKey)
			} else {
				s.nodes[outcome.job.Advert.PublicKey] = state
			}
			if outcome.err != nil {
				s.logger.Warn("map upload failed", "packet_id", outcome.job.Advert.PacketID, "node", outcome.job.Advert.PublicKey, "error", outcome.err)
			}
		case <-ticker.C:
			s.sweep(time.Now())
		}
	}
}

func (s *Service) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-s.jobs:
			handled, err := s.tryUpload(ctx, job)
			select {
			case s.results <- result{job: job, handled: handled, err: err}:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (s *Service) tryUpload(ctx context.Context, job Job) (bool, error) {
	var last error
	for attempt := 1; attempt <= s.cfg.Attempts; attempt++ {
		handled, retry, err := s.poster.Post(ctx, job, attempt)
		if err != nil {
			s.logger.Warn("map upload attempt failed", "packet_id", job.Advert.PacketID, "node", job.Advert.PublicKey, "attempt", attempt, "retry", retry, "error", err)
		}
		if err == nil || !retry {
			if err == nil {
				if s.cfg.DryRun {
					s.logger.Info("advert validated in dry run", "packet_id", job.Advert.PacketID, "node", job.Advert.PublicKey, "name", job.Advert.Name, "type", job.Advert.Type, "repeat_sightings_ignored_for", "72h")
				} else {
					s.logger.Info("map advert handled", "packet_id", job.Advert.PacketID, "node", job.Advert.PublicKey, "name", job.Advert.Name, "type", job.Advert.Type, "repeat_sightings_ignored_for", "72h")
				}
			}
			return handled, err
		}
		last = err
		if attempt < s.cfg.Attempts {
			wait := s.cfg.RetryDelay * time.Duration(1<<(attempt-1))
			if wait > time.Minute {
				wait = time.Minute
			}
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return false, ctx.Err()
			}
		}
	}
	return false, last
}
