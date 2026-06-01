package mcphttp

import (
	"context"

	"wowdata/internal/server/health"
)

func HealthSnapshot(ctx context.Context, provider health.Provider) (health.Snapshot, error) {
	return provider.HealthSnapshot(ctx)
}

func WowStatus(ctx context.Context, provider health.Provider) (health.Snapshot, error) {
	return provider.HealthSnapshot(ctx)
}
