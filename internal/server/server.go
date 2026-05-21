package server

import (
	"context"
	"log/slog"

	"github.com/zesbe/HallowaBackend/internal/config"
	"github.com/zesbe/HallowaBackend/internal/supabase"
)

// Identity holds this backend's server row in backend_servers.
type Identity struct {
	ID         string
	Name       string
	URL        string
	Region     string
	Priority   int
	Capacity   int
	currentLoad int
}

// Register finds-or-creates this server in backend_servers and returns its identity.
// Logic mirrors the Node serverAssignmentService.registerServer():
//   - look up by server_url first
//   - if found: update is_active/is_healthy/priority/name/region
//   - else: insert new row
func Register(ctx context.Context, sb *supabase.Client, cfg *config.Config, log *slog.Logger) (*Identity, error) {
	existing, err := sb.FindServerByURL(ctx, cfg.ServerURL)
	if err != nil {
		return nil, err
	}

	row := supabase.BackendServer{
		ServerName:     cfg.ServerName,
		ServerURL:      cfg.ServerURL,
		ServerType:     "vps",
		Region:         cfg.ServerRegion,
		MaxCapacity:    cfg.ServerMaxCapacity,
		CurrentLoad:    0,
		IsActive:       true,
		IsHealthy:      true,
		Priority:       cfg.ServerPriority,
		HealthFailures: 0,
		Metadata: map[string]any{
			"runtime":  "go",
			"backend":  "whatsmeow",
			"capable":  []string{"qr", "pairing", "send"},
		},
	}
	if existing != nil {
		row.ID = existing.ID
		log.Info("found existing server row", "id", existing.ID, "name", existing.ServerName)
	} else {
		log.Info("creating new server row", "name", cfg.ServerName, "url", cfg.ServerURL)
	}

	saved, err := sb.UpsertServer(ctx, row)
	if err != nil {
		return nil, err
	}

	log.Info("server registered", "id", saved.ID, "name", saved.ServerName, "priority", saved.Priority)
	return &Identity{
		ID:       saved.ID,
		Name:     saved.ServerName,
		URL:      saved.ServerURL,
		Region:   saved.Region,
		Priority: saved.Priority,
		Capacity: saved.MaxCapacity,
	}, nil
}
