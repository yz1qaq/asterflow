package modelconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	pgstore "github.com/yz1qaq/asterflow/internal/infrastructure/postgres/sqlc"
)

var (
	ErrProfileNameExists      = errors.New("model profile name already exists")
	ErrProfileNotFound        = errors.New("model profile not found")
	ErrProfileVersionNotFound = errors.New("model profile version not found")
)

func profileFromRow(row pgstore.ModelProfile) *ModelProfile {
	return &ModelProfile{
		ID:          row.ID.String(),
		WorkspaceID: row.WorkspaceID,
		Name:        row.Name,
		Description: row.Description,
		CreatedBy:   row.CreatedBy,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

func encodeFallbackDeploymentIDs(ids []string) ([]byte, error) {
	if ids == nil {
		ids = []string{}
	}
	return json.Marshal(ids)
}

func profileVersionFromRow(
	row pgstore.ModelProfileVersion,
) (*ModelProfileVersion, error) {
	var fallbacks []string
	if err := json.Unmarshal(
		row.FallbackDeploymentIds,
		&fallbacks,
	); err != nil {
		return nil, fmt.Errorf("decode profile fallback deployments: %w", err)
	}
	if fallbacks == nil {
		return nil, fmt.Errorf("stored fallback deployments must be a JSON array")
	}

	return &ModelProfileVersion{
		ID:                    row.ID.String(),
		ProfileID:             row.ProfileID.String(),
		Version:               int(row.Version),
		PrimaryDeploymentID:   row.PrimaryDeploymentID.String(),
		FallbackDeploymentIDs: fallbacks,
		ClassificationMax:     row.ClassificationMax,
		CreatedBy:             row.CreatedBy,
		CreatedAt:             row.CreatedAt,
	}, nil
}

func (s *PostgresStore) CreateProfileWithVersion(
	ctx context.Context,
	profile *ModelProfile,
	version *ModelProfileVersion,
) error {
	if profile == nil || version == nil {
		return fmt.Errorf("profile and initial version are required")
	}

	profileID, err := uuid.Parse(profile.ID)
	if err != nil {
		return fmt.Errorf("parse profile id: %w", err)
	}
	versionID, err := uuid.Parse(version.ID)
	if err != nil {
		return fmt.Errorf("parse profile version id: %w", err)
	}
	parentID, err := uuid.Parse(version.ProfileID)
	if err != nil {
		return fmt.Errorf("parse version profile id: %w", err)
	}

	if parentID != profileID || version.Version != 1 {
		return fmt.Errorf(
			"initial version must belong to the profile and have version number 1",
		)
	}

	primaryID, err := uuid.Parse(version.PrimaryDeploymentID)
	if err != nil {
		return fmt.Errorf("parse primary deployment id: %w", err)
	}

	fallbacks, err := encodeFallbackDeploymentIDs(
		version.FallbackDeploymentIDs,
	)
	if err != nil {
		return fmt.Errorf("encode profile fallback deployments: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin profile creation: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	q := s.q.WithTx(tx)

	profileRow, err := q.CreateProfile(ctx, pgstore.CreateProfileParams{
		ID:          profileID,
		WorkspaceID: profile.WorkspaceID,
		Name:        profile.Name,
		Description: profile.Description,
		CreatedBy:   profile.CreatedBy,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" &&
			pgErr.ConstraintName == "model_profiles_workspace_id_name_key" {
			return ErrProfileNameExists
		}
		return fmt.Errorf("create model profile: %w", err)
	}

	versionRow, err := q.CreateProfileVersion(
		ctx,
		pgstore.CreateProfileVersionParams{
			ID:                    versionID,
			ProfileID:             profileID,
			Version:               1,
			PrimaryDeploymentID:   primaryID,
			FallbackDeploymentIds: fallbacks,
			ClassificationMax:     version.ClassificationMax,
			CreatedBy:             version.CreatedBy,
		},
	)
	if err != nil {
		return fmt.Errorf("create initial profile version: %w", err)
	}

	savedVersion, err := profileVersionFromRow(versionRow)
	if err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit profile creation: %w", err)
	}

	*profile = *profileFromRow(profileRow)
	*version = *savedVersion
	return nil
}

func (s *PostgresStore) GetProfile(
	ctx context.Context, profileID string,
) (*ModelProfile, error) {
	id, err := uuid.Parse(profileID)
	if err != nil {
		return nil, fmt.Errorf("parse profile id: %w", err)
	}
	row, err := s.q.GetProfile(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrProfileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get model profile: %w", err)
	}
	return profileFromRow(row), nil
}

func (s *PostgresStore) ListProfiles(
	ctx context.Context, workspaceID string,
) ([]*ModelProfile, error) {
	rows, err := s.q.ListProfiles(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list model profiles: %w", err)
	}
	profiles := make([]*ModelProfile, 0, len(rows))
	for _, row := range rows {
		profiles = append(profiles, profileFromRow(row))
	}
	return profiles, nil
}

func (s *PostgresStore) GetProfileVersion(
	ctx context.Context, versionID string,
) (*ModelProfileVersion, error) {
	id, err := uuid.Parse(versionID)
	if err != nil {
		return nil, fmt.Errorf("parse profile version id: %w", err)
	}
	row, err := s.q.GetProfileVersion(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrProfileVersionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get model profile version: %w", err)
	}
	return profileVersionFromRow(row)
}
func (s *PostgresStore) ListProfileVersions(
	ctx context.Context, profileID string,
) ([]*ModelProfileVersion, error) {
	id, err := uuid.Parse(profileID)
	if err != nil {
		return nil, fmt.Errorf("parse profile id: %w", err)
	}

	rows, err := s.q.ListProfileVersions(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list model profile versions: %w", err)
	}
	versions := make([]*ModelProfileVersion, 0, len(rows))
	for _, row := range rows {
		version, err := profileVersionFromRow(row)
		if err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	return versions, nil
}

func (s *PostgresStore) CreateProfileVersion(
	ctx context.Context,
	version *ModelProfileVersion,
) error {
	if version == nil {
		return fmt.Errorf("profile version is required")
	}

	id, err := uuid.Parse(version.ID)
	if err != nil {
		return fmt.Errorf("parse profile version id: %w", err)
	}
	profileID, err := uuid.Parse(version.ProfileID)
	if err != nil {
		return fmt.Errorf("parse profile id: %w", err)
	}
	primaryID, err := uuid.Parse(version.PrimaryDeploymentID)
	if err != nil {
		return fmt.Errorf("parse primary deployment id: %w", err)
	}

	fallbacks, err := encodeFallbackDeploymentIDs(
		version.FallbackDeploymentIDs,
	)
	if err != nil {
		return fmt.Errorf("encode profile fallback deployments: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin profile version creation: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	q := s.q.WithTx(tx)
	//先锁住,如果不锁住,可能版本号会重复,比如得到版本号以后还没存入,就被其他存入了
	if _, err := q.LockModelProfile(ctx, profileID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrProfileNotFound
		}
		return fmt.Errorf("lock model profile: %w", err)
	}
	//得到版本号
	next, err := q.NextModelProfileVersion(ctx, profileID)
	if err != nil {
		return fmt.Errorf("allocate profile version number: %w", err)
	}
	if next < 1 || next > math.MaxInt32 {
		return fmt.Errorf("profile version number is out of range")
	}

	row, err := q.CreateProfileVersion(
		ctx,
		pgstore.CreateProfileVersionParams{
			ID:                    id,
			ProfileID:             profileID,
			Version:               int32(next),
			PrimaryDeploymentID:   primaryID,
			FallbackDeploymentIds: fallbacks,
			ClassificationMax:     version.ClassificationMax,
			CreatedBy:             version.CreatedBy,
		},
	)
	if err != nil {
		return fmt.Errorf("create model profile version: %w", err)
	}
	saved, err := profileVersionFromRow(row)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit profile version creation: %w", err)
	}
	*version = *saved
	return nil
}
