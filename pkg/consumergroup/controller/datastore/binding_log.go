package datastore

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	iTopic "github.com/agentstax/vulkan/internal/topic"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/jackc/pgx/v5"
)

// DeclareBindings states the group's full binding set in one transaction and
// reports the end state (see classifyDeclaration). patterns must arrive
// sorted and deduplicated -- sets are compared element-wise.
func (d *ConsumerGroupDatastore) DeclareBindings(ctx context.Context, topicId int64, groupId int64, patterns []string, declaredBy string, declaredAt time.Time) (consumergroup.DeclarationOutcome, error) {
	var outcome consumergroup.DeclarationOutcome
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		outcome, err = d.declareBindings(ctx, topicId, groupId, patterns, declaredBy, declaredAt)
		return err
	})
	return outcome, err
}

func (d *ConsumerGroupDatastore) declareBindings(ctx context.Context, topicId int64, groupId int64, patterns []string, declaredBy string, declaredAt time.Time) (consumergroup.DeclarationOutcome, error) {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	// installed rows have no uniqueness; this row lock is what serializes
	// concurrent installers on the group
	lockSql := `
		-- vulkan: consumergroup.declareBindings
		SELECT id
		FROM consumer_group_config
		WHERE id = $1
		FOR UPDATE;
	`
	var lockedGroupId int64
	if err := tx.QueryRow(ctx, lockSql, groupId).Scan(&lockedGroupId); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", consumergroup.ErrGroupNotFound.With("group_id", groupId)
		}
		return "", err
	}

	declarations, err := d.listTopicBindingLog(ctx, tx, topicId, groupId)
	if err != nil {
		return "", err
	}
	installed, found := NewestInstalledDeclaration(declarations)

	live, err := d.groupHasLiveInstance(ctx, tx, groupId)
	if err != nil {
		return "", err
	}
	var storedPatterns []string
	if found {
		storedPatterns = installed.Patterns
	}

	outcome := classifyDeclaration(found, storedPatterns, patterns, live)
	switch outcome {
	case consumergroup.DeclarationJoined:
		// the stored set already matches -- nothing to write
	case consumergroup.DeclarationWaiting:
		if err := d.appendDeclaration(ctx, tx, topicId, groupId, BindingLogWaiting, patterns, declaredBy, declaredAt); err != nil {
			return "", err
		}
	case consumergroup.DeclarationInstalled:
		if err := d.appendDeclaration(ctx, tx, topicId, groupId, BindingLogInstalled, patterns, declaredBy, declaredAt); err != nil {
			return "", err
		}
		if err := d.replaceBindings(ctx, tx, topicId, groupId, patterns); err != nil {
			return "", err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return "", err
	}

	if outcome == consumergroup.DeclarationInstalled {
		d.Logger.InfoContext(ctx, "binding set installed", "group_id", groupId, "patterns", patterns, "previous_patterns", storedPatterns, "declared_by", declaredBy)
	}
	return outcome, nil
}

// groupHasLiveInstance: a fresh heartbeat is a live instance still declaring
// the stored set.
func (d *ConsumerGroupDatastore) groupHasLiveInstance(ctx context.Context, tx pgx.Tx, groupId int64) (bool, error) {
	sql := `
		-- vulkan: consumergroup.groupHasLiveInstance
		SELECT EXISTS (
			SELECT 1
			FROM worker_instance
			JOIN worker_config ON worker_config.id = worker_instance.worker_id
			WHERE worker_config.consumer_group_id = $1
			AND worker_instance.expires_at > now()
		);
	`

	var live bool
	err := tx.QueryRow(ctx, sql, groupId).Scan(&live)
	return live, err
}

// appendDeclaration writes one attempt row; attempted_at is the insert's now().
func (d *ConsumerGroupDatastore) appendDeclaration(ctx context.Context, tx pgx.Tx, topicId int64, groupId int64, status BindingLogStatus, patterns []string, declaredBy string, declaredAt time.Time) error {
	sql := fmt.Sprintf(`
		-- vulkan: consumergroup.appendDeclaration
		INSERT INTO %s (consumer_group_id, status, patterns, declared_by, declared_at)
		VALUES ($1, $2, $3, $4, $5);
	`, iTopic.BindingConfigLogTable(topicId))
	_, err := tx.Exec(ctx, sql, groupId, status, patterns, declaredBy, declaredAt)
	return err
}

func (d *ConsumerGroupDatastore) replaceBindings(ctx context.Context, tx pgx.Tx, topicId int64, groupId int64, patterns []string) error {
	deleteSql := fmt.Sprintf(`
		-- vulkan: consumergroup.replaceBindings
		DELETE FROM %s
		WHERE consumer_group_id = $1;
	`, iTopic.BindingConfigTable(topicId))
	if _, err := tx.Exec(ctx, deleteSql, groupId); err != nil {
		return err
	}

	insertSql := fmt.Sprintf(`
		-- vulkan: consumergroup.replaceBindings
		INSERT INTO %s (consumer_group_id, pattern_regex, pattern)
		VALUES ($1, $2, $3);
	`, iTopic.BindingConfigTable(topicId))
	for _, pattern := range patterns {
		expression := wildcardToRegex(pattern)
		if _, err := tx.Exec(ctx, insertSql, groupId, expression, pattern); err != nil {
			return err
		}
	}
	return nil
}

// ListBindingLog reads every group's newest attempt row per declarer
// and status, with the names a listing shows -- one query per topic's
// binding_config_log table.
func (d *ConsumerGroupDatastore) ListBindingLog(ctx context.Context) ([]BindingConfigLogRow, error) {
	var declarations []BindingConfigLogRow
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		declarations, err = d.listBindingLog(ctx)
		return err
	})
	return declarations, err
}

func (d *ConsumerGroupDatastore) listBindingLog(ctx context.Context) ([]BindingConfigLogRow, error) {
	topicIds, err := d.listGroupTopicIds(ctx, d.Datastore.Pool)
	if err != nil {
		return nil, err
	}

	var declarations []BindingConfigLogRow
	for _, topicId := range topicIds {
		topicDeclarations, err := d.listTopicBindingLog(ctx, d.Datastore.Pool, topicId, 0)
		if err != nil {
			return nil, err
		}
		declarations = append(declarations, topicDeclarations...)
	}
	return declarations, nil
}

func (d *ConsumerGroupDatastore) listTopicBindingLog(ctx context.Context, querier datastore.Querier, topicId int64, groupId int64) ([]BindingConfigLogRow, error) {
	// DISTINCT ON keeps newest-per-declarer in SQL -- a long wait's appended
	// retry rows never ship to the caller
	sql := fmt.Sprintf(`
		-- vulkan: consumergroup.listTopicBindingLog
		SELECT DISTINCT ON (binding_config_log.consumer_group_id, binding_config_log.status, binding_config_log.declared_by)
			binding_config_log.id,
			binding_config_log.consumer_group_id,
			consumer_group_config.name,
			topic_config.name,
			binding_config_log.status,
			binding_config_log.patterns,
			binding_config_log.declared_by,
			binding_config_log.declared_at,
			binding_config_log.attempted_at
		FROM %s binding_config_log
		JOIN consumer_group_config ON consumer_group_config.id = binding_config_log.consumer_group_id
		JOIN topic_config ON topic_config.id = consumer_group_config.topic_id
		-- $1 = 0 -> every group
		WHERE ($1 = 0 OR binding_config_log.consumer_group_id = $1)
		ORDER BY binding_config_log.consumer_group_id, binding_config_log.status, binding_config_log.declared_by, binding_config_log.id DESC;
	`, iTopic.BindingConfigLogTable(topicId))
	rows, err := querier.Query(ctx, sql, groupId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var declarations []BindingConfigLogRow
	for rows.Next() {
		var declaration BindingConfigLogRow
		if err := rows.Scan(
			&declaration.Id,
			&declaration.ConsumerGroupId,
			&declaration.GroupName,
			&declaration.TopicName,
			&declaration.Status,
			&declaration.Patterns,
			&declaration.DeclaredBy,
			&declaration.DeclaredAt,
			&declaration.AttemptedAt,
		); err != nil {
			return nil, err
		}
		declarations = append(declarations, declaration)
	}
	return declarations, rows.Err()
}

// listGroupTopicIds is every topic id with registered groups. A
// binding_config_log row cascades with its group, so these topics cover
// every declaration.
func (d *ConsumerGroupDatastore) listGroupTopicIds(ctx context.Context, querier datastore.Querier) ([]int64, error) {
	sql := `
		-- vulkan: consumergroup.listGroupTopicIds
		SELECT DISTINCT topic_id
		FROM consumer_group_config
		ORDER BY topic_id;
	`
	rows, err := querier.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var topicIds []int64
	for rows.Next() {
		var topicId int64
		if err := rows.Scan(&topicId); err != nil {
			return nil, err
		}
		topicIds = append(topicIds, topicId)
	}
	return topicIds, rows.Err()
}

// NewestInstalledDeclaration picks the highest-id installed row -- the
// effective set's declaration.
func NewestInstalledDeclaration(declarations []BindingConfigLogRow) (*BindingConfigLogRow, bool) {
	var newest *BindingConfigLogRow
	for i := range declarations {
		if declarations[i].Status != BindingLogInstalled {
			continue
		}
		if newest == nil || declarations[i].Id > newest.Id {
			newest = &declarations[i]
		}
	}
	return newest, newest != nil
}

// ***************
// *** HELPERS ***
// ***************

func classifyDeclaration(found bool, storedPatterns []string, patterns []string, live bool) consumergroup.DeclarationOutcome {
	switch {
	case !found:
		return consumergroup.DeclarationInstalled // first declarer wins
	case equalPatterns(storedPatterns, patterns):
		return consumergroup.DeclarationJoined
	case !live:
		return consumergroup.DeclarationInstalled // nothing live declares the stored set
	default:
		// waiting never changes the effective set -- a missed case blocks
		// loudly instead of installing silently
		return consumergroup.DeclarationWaiting
	}
}

// translates a '*'-wildcard pattern into an anchored POSIX regex suitable for
// the `~` operator: '*' -> `.*` (any characters, unbounded), literal segments
// regex-escaped.
func wildcardToRegex(pattern string) string {
	segments := strings.Split(pattern, "*")
	var builder strings.Builder
	builder.WriteByte('^')
	for i, segment := range segments {
		if i > 0 {
			builder.WriteString(".*")
		}
		builder.WriteString(regexp.QuoteMeta(segment))
	}
	builder.WriteByte('$')
	return builder.String()
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
