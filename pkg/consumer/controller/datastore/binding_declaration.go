package datastore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/consumer/binding"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/jackc/pgx/v5"
)

// DeclareBindings states the group's full binding set in one transaction and
// reports the end state (see classifyDeclaration). patterns must arrive
// sorted and deduplicated -- sets are compared element-wise.
func (d *ConsumerDatastore) DeclareBindings(ctx context.Context, groupID int64, patterns []string, declaredBy string, declaredAt time.Time) (binding.DeclarationOutcome, error) {
	var outcome binding.DeclarationOutcome
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		outcome, err = d.declareBindings(ctx, groupID, patterns, declaredBy, declaredAt)
		return err
	})
	return outcome, err
}

func (d *ConsumerDatastore) declareBindings(ctx context.Context, groupID int64, patterns []string, declaredBy string, declaredAt time.Time) (binding.DeclarationOutcome, error) {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	// installed rows have no uniqueness; this row lock is what serializes
	// concurrent installers on the group
	lockSql := `
		SELECT id
		FROM consumer_group
		WHERE id = $1
		FOR UPDATE;
	`
	var lockedGroupID int64
	if err := tx.QueryRow(ctx, lockSql, groupID).Scan(&lockedGroupID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("consumer group %d is not registered", groupID)
		}
		return "", err
	}

	installedRows, err := d.listDeclarations(ctx, tx, groupID, BindingDeclarationInstalled)
	if err != nil {
		return "", err
	}
	installed, found := newestDeclaration(installedRows)

	live, err := d.groupHasLiveInstance(ctx, tx, groupID)
	if err != nil {
		return "", err
	}
	var storedPatterns []string
	if found {
		storedPatterns = installed.Patterns
	}

	outcome := classifyDeclaration(found, storedPatterns, patterns, live)
	switch outcome {
	case binding.DeclarationJoined:
		// the stored set already matches -- nothing to write
	case binding.DeclarationWaiting:
		if err := d.appendDeclaration(ctx, tx, groupID, BindingDeclarationWaiting, patterns, declaredBy, declaredAt); err != nil {
			return "", err
		}
	case binding.DeclarationInstalled:
		if err := d.appendDeclaration(ctx, tx, groupID, BindingDeclarationInstalled, patterns, declaredBy, declaredAt); err != nil {
			return "", err
		}
		if err := d.replaceBindings(ctx, tx, groupID, patterns); err != nil {
			return "", err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return "", err
	}

	if outcome == binding.DeclarationInstalled {
		d.Logger.InfoContext(ctx, "binding set installed", "group_id", groupID, "patterns", patterns, "previous_patterns", storedPatterns, "declared_by", declaredBy)
	}
	return outcome, nil
}

func classifyDeclaration(found bool, storedPatterns []string, patterns []string, live bool) binding.DeclarationOutcome {
	switch {
	case !found:
		return binding.DeclarationInstalled // first declarer wins
	case equalPatterns(storedPatterns, patterns):
		return binding.DeclarationJoined
	case !live:
		return binding.DeclarationInstalled // nothing live declares the stored set
	default:
		// waiting never changes the effective set -- a missed case blocks
		// loudly instead of installing silently
		return binding.DeclarationWaiting
	}
}

// groupHasLiveInstance: a fresh heartbeat is a live instance still declaring
// the stored set.
func (d *ConsumerDatastore) groupHasLiveInstance(ctx context.Context, tx pgx.Tx, groupID int64) (bool, error) {
	sql := `
		SELECT EXISTS (
			SELECT 1
			FROM worker_instance
			JOIN worker ON worker.id = worker_instance.worker_id
			WHERE worker.consumer_group_id = $1
			AND worker_instance.expires_at > now()
		);
	`

	var live bool
	err := tx.QueryRow(ctx, sql, groupID).Scan(&live)
	return live, err
}

// appendDeclaration writes one attempt row; attempt_at is the insert's now().
func (d *ConsumerDatastore) appendDeclaration(ctx context.Context, tx pgx.Tx, groupID int64, status BindingDeclarationStatus, patterns []string, declaredBy string, declaredAt time.Time) error {
	sql := `
		INSERT INTO binding_declaration (consumer_group_id, status, patterns, declared_by, declared_at)
		VALUES ($1, $2, $3, $4, $5);
	`

	_, err := tx.Exec(ctx, sql, groupID, status, patterns, declaredBy, declaredAt)
	return err
}

func (d *ConsumerDatastore) replaceBindings(ctx context.Context, tx pgx.Tx, groupID int64, patterns []string) error {
	deleteSql := `
		DELETE FROM binding
		WHERE consumer_group_id = $1;
	`
	if _, err := tx.Exec(ctx, deleteSql, groupID); err != nil {
		return err
	}

	insertSql := `
		INSERT INTO binding (consumer_group_id, pattern, display)
		VALUES ($1, $2, $3);
	`
	for _, display := range patterns {
		expression, err := wildcardToRegex(display)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, insertSql, groupID, expression, display); err != nil {
			return err
		}
	}
	return nil
}

// ListDeclarations reads the group's newest attempt row per declarer with
// the given status.
func (d *ConsumerDatastore) ListDeclarations(ctx context.Context, groupID int64, bindingDeclarationStatus BindingDeclarationStatus) ([]BindingDeclarationData, error) {
	var declarations []BindingDeclarationData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		declarations, err = d.listDeclarations(ctx, d.Datastore.Pool, groupID, bindingDeclarationStatus)
		return err
	})
	return declarations, err
}

func (d *ConsumerDatastore) listDeclarations(ctx context.Context, querier datastore.Querier, groupID int64, bindingDeclarationStatus BindingDeclarationStatus) ([]BindingDeclarationData, error) {
	// DISTINCT ON keeps newest-per-declarer in SQL -- a long wait's appended
	// retry rows never ship to the caller
	sql := `
		SELECT DISTINCT ON (declared_by)
			id,
			consumer_group_id,
			status,
			patterns,
			declared_by,
			declared_at,
			attempt_at
		FROM binding_declaration
		WHERE consumer_group_id = $1 AND status = $2
		ORDER BY declared_by, id DESC;
	`

	rows, err := querier.Query(ctx, sql, groupID, bindingDeclarationStatus)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var declarations []BindingDeclarationData
	for rows.Next() {
		var declaration BindingDeclarationData
		if err := rows.Scan(
			&declaration.Id,
			&declaration.ConsumerGroupId,
			&declaration.Status,
			&declaration.Patterns,
			&declaration.DeclaredBy,
			&declaration.DeclaredAt,
			&declaration.AttemptAt,
		); err != nil {
			return nil, err
		}
		declarations = append(declarations, declaration)
	}
	return declarations, rows.Err()
}

// newestDeclaration picks the highest-id row; among installed rows that is
// the effective set's declaration.
func newestDeclaration(declarations []BindingDeclarationData) (*BindingDeclarationData, bool) {
	var newest *BindingDeclarationData
	for i := range declarations {
		if newest == nil || declarations[i].Id > newest.Id {
			newest = &declarations[i]
		}
	}
	return newest, newest != nil
}

// equalPatterns compares two sorted, deduplicated sets element-wise.
func equalPatterns(stored []string, declared []string) bool {
	if len(stored) != len(declared) {
		return false
	}
	for i := range stored {
		if stored[i] != declared[i] {
			return false
		}
	}
	return true
}
