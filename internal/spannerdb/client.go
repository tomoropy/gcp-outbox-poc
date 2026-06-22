package spannerdb

import (
	"context"

	"cloud.google.com/go/spanner"
)

func NewClient(ctx context.Context, databasePath string) (*spanner.Client, error) {
	return spanner.NewClient(ctx, databasePath)
}
