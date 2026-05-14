package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// MemoryCompaction represents a group of related observations that have been summarized.
type MemoryCompaction struct {
	ID             int64     `json:"id"`
	SourceIDs      []int64   `json:"source_ids"`
	SummaryTitle   string    `json:"summary_title"`
	SummaryContent string    `json:"summary_content"`
	Type           string    `json:"type"`
	Project        *string   `json:"project,omitempty"`
	Scope          string    `json:"scope"`
	CreatedAt      string    `json:"created_at"`
}

// FindCompactionCandidates returns groups of related observations that could be compacted.
// It looks for observations with the same type, similar project, and older than maxAge.
// When minGroupSize observations of the same type+project exist beyond maxAge, they form a candidate group.
func (s *Store) FindCompactionCandidates(maxAge time.Duration, minGroupSize int) ([]MemoryCompaction, error) {
	if minGroupSize <= 0 {
		minGroupSize = 3
	}

	rows, err := s.queryItHook(s.db, `
		SELECT type, project, scope, COUNT(*) as cnt,
		       GROUP_CONCAT(CAST(id AS TEXT)) as ids,
		       MIN(created_at) as oldest,
		       MAX(created_at) as newest
		FROM observations
		WHERE deleted_at IS NULL
		  AND datetime(created_at) <= datetime('now', ?)
		GROUP BY type, ifnull(project, ''), scope
		HAVING cnt >= ?
		ORDER BY cnt DESC
	`, fmt.Sprintf("-%d days", int(maxAge.Hours()/24)), minGroupSize)
	if err != nil {
		return nil, fmt.Errorf("find compaction candidates: %w", err)
	}
	defer rows.Close()

	var groups []MemoryCompaction
	for rows.Next() {
		var typ, scope, idsStr string
		var project *string
		var cnt int
		var oldest, newest string
		if err := rows.Scan(&typ, &project, &scope, &cnt, &idsStr, &oldest, &newest); err != nil {
			return nil, fmt.Errorf("scan compaction candidate: %w", err)
		}

		// Parse the comma-separated IDs
		var sourceIDs []int64
		for _, idStr := range strings.Split(idsStr, ",") {
			idStr = strings.TrimSpace(idStr)
			if idStr == "" {
				continue
			}
			var id int64
			if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
				continue
			}
			sourceIDs = append(sourceIDs, id)
		}

		groups = append(groups, MemoryCompaction{
			Type:      typ,
			Project:   project,
			Scope:     scope,
			SourceIDs: sourceIDs,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("compaction candidates iteration: %w", err)
	}

	return groups, nil
}

// ExecuteCompaction creates a summary observation from a group of old observations.
// The source observations are marked as superseded so they don't appear in normal searches.
func (s *Store) ExecuteCompaction(group MemoryCompaction, summaryTitle, summaryContent string) error {
	if len(group.SourceIDs) == 0 {
		return fmt.Errorf("compaction: no source IDs to compact")
	}

	// Build the JSON array of source observation IDs for storage
	sourceIDsJSON, err := json.Marshal(group.SourceIDs)
	if err != nil {
		return fmt.Errorf("marshal source IDs: %w", err)
	}

	// Determine scope and project for the compaction record
	scope := group.Scope
	if scope == "" {
		scope = "project"
	}

	projectJSON := "NULL"
	if group.Project != nil && *group.Project != "" {
		escaped := strings.ReplaceAll(*group.Project, "'", "''")
		projectJSON = fmt.Sprintf("'%s'", escaped)
	}

	return s.withTx(func(tx *sql.Tx) error {
		// Insert the compaction record
		_, err := s.execHook(tx, `
			INSERT INTO memory_compactions (source_ids, summary_title, summary_content, type, project, scope)
			VALUES (?, ?, ?, ?, ?, ?)
		`, string(sourceIDsJSON), summaryTitle, summaryContent, group.Type, projectJSON, scope)
		if err != nil {
			return fmt.Errorf("insert compaction: %w", err)
		}

		// Mark source observations as superseded
		for _, id := range group.SourceIDs {
			if _, err := s.execHook(tx,
				`UPDATE observations SET superseded_at = datetime('now') WHERE id = ? AND deleted_at IS NULL`,
				id,
			); err != nil {
				return fmt.Errorf("supersede observation %d: %w", id, err)
			}
		}

		return nil
	})
}

// GetCompactions returns all compaction summaries, optionally filtered by scope.
func (s *Store) GetCompactions(scope string, limit int) ([]MemoryCompaction, error) {
	if limit <= 0 {
		limit = 10
	}

	query := `
		SELECT id, source_ids, summary_title, summary_content, type, project, scope, created_at
		FROM memory_compactions
		WHERE 1=1
	`
	args := []any{}

	if scope != "" {
		query += " AND scope = ?"
		args = append(args, scope)
	}

	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.queryItHook(s.db, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get compactions: %w", err)
	}
	defer rows.Close()

	var results []MemoryCompaction
	for rows.Next() {
		var mc MemoryCompaction
		var sourceIDsJSON string
		if err := rows.Scan(&mc.ID, &sourceIDsJSON, &mc.SummaryTitle, &mc.SummaryContent, &mc.Type, &mc.Project, &mc.Scope, &mc.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan compaction: %w", err)
		}
		if err := json.Unmarshal([]byte(sourceIDsJSON), &mc.SourceIDs); err != nil {
			return nil, fmt.Errorf("parse source IDs: %w", err)
		}
		results = append(results, mc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("compactions iteration: %w", err)
	}

	return results, nil
}

// CountCompactionCandidates returns the number of groups that would be candidates for compaction.
func (s *Store) CountCompactionCandidates(maxAge time.Duration, minGroupSize int) (int, error) {
	if minGroupSize <= 0 {
		minGroupSize = 3
	}

	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM (
			SELECT type, project, scope, COUNT(*) as cnt
			FROM observations
			WHERE deleted_at IS NULL
			  AND datetime(created_at) <= datetime('now', ?)
			GROUP BY type, ifnull(project, ''), scope
			HAVING cnt >= ?
		)
	`, fmt.Sprintf("-%d days", int(maxAge.Hours()/24)), minGroupSize).Scan(&count)

	return count, err
}

// ObservationDateRange returns the min/max created_at for a list of observation IDs.
func (s *Store) ObservationDateRange(ids []int64) (oldest, newest string) {
	if len(ids) == 0 {
		return "N/A", "N/A"
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	var o, n *string
	err := s.db.QueryRow(
		fmt.Sprintf("SELECT MIN(created_at), MAX(created_at) FROM observations WHERE id IN (%s)", placeholders),
		args...,
	).Scan(&o, &n)
	if err != nil || o == nil || n == nil {
		return "N/A", "N/A"
	}
	return *o, *n
}
