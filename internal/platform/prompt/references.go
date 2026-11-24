package prompt

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	pgstore "github.com/yz1qaq/asterflow/internal/infrastructure/postgres/sqlc"
)

// LockVersionForReference holds a live prompt version until the caller saves
// its reference and commits the transaction.
func LockVersionForReference(ctx context.Context, q *pgstore.Queries, workspaceID string, versionID uuid.UUID) error {
	_, err := q.LockLivePromptVersion(ctx, pgstore.LockLivePromptVersionParams{
		WorkspaceID: workspaceID, VersionID: versionID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPromptVersionNotFound
	}
	return err
}
