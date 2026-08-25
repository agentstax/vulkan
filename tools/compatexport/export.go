package main

import (
	"fmt"

	"github.com/agentstax/vulkan/pkg/migrate"
)

// Export is the build-time artifact the doc site's compatibility matrix
// reads: one grid per schema scope, every cell already decided by the
// library's own rule so the page holds no second copy of it.
type Export struct {
	System *ScopeExport `json:"system"`
	Topic  *ScopeExport `json:"topic"`
}

func NewExport(systemRegistry []migrate.Migration, topicRegistry []migrate.Migration) (*Export, error) {
	system, err := newScopeExport(systemRegistry)
	if err != nil {
		return nil, fmt.Errorf("system: %w", err)
	}
	topic, err := newScopeExport(topicRegistry)
	if err != nil {
		return nil, fmt.Errorf("topic: %w", err)
	}
	return &Export{System: system, Topic: topic}, nil
}

// ScopeExport is one scope's whole grid: the version this registry defines,
// the steps that built it, and a row per schema version a database could sit
// at.
type ScopeExport struct {
	Version int64        `json:"version"`
	Steps   []StepExport `json:"steps"`
	Rows    []RowExport  `json:"rows"`
}

func newScopeExport(registry []migrate.Migration) (*ScopeExport, error) {
	if err := migrate.Validate(registry); err != nil {
		return nil, err
	}

	current := migrate.Version(registry)
	steps := make([]StepExport, 0, len(registry))
	for _, step := range registry {
		steps = append(steps, StepExport{Version: step.Version, MinCompatibleVersion: step.MinCompatibleVersion})
	}

	rows := make([]RowExport, 0, current)
	for version := int64(1); version <= current; version++ {
		rows = append(rows, newRowExport(registry, version, current))
	}

	return &ScopeExport{Version: current, Steps: steps, Rows: rows}, nil
}

// StepExport is one registry step. MinCompatibleVersion 0 marks it additive;
// anything else is the floor it raises.
type StepExport struct {
	Version              int64 `json:"version"`
	MinCompatibleVersion int64 `json:"min_compatible_version"`
}

// RowExport is every verdict for one database version: which builds it
// admits, and the floor in force at that version.
type RowExport struct {
	Version              int64        `json:"version"`
	MinCompatibleVersion int64        `json:"min_compatible_version"`
	Cells                []CellExport `json:"cells"`
}

func newRowExport(registry []migrate.Migration, version int64, current int64) RowExport {
	floor := minCompatibleVersionAt(registry, version)

	cells := make([]CellExport, 0, current)
	for buildVersion := int64(1); buildVersion <= current; buildVersion++ {
		support := migrate.ClassifySchemaSupport(version, floor, buildVersion)
		cells = append(cells, CellExport{BuildVersion: buildVersion, Support: support})
	}

	return RowExport{Version: version, MinCompatibleVersion: floor, Cells: cells}
}

// CellExport is one (database version, build version) pair's verdict.
type CellExport struct {
	BuildVersion int64                 `json:"build_version"`
	Support      migrate.SchemaSupport `json:"support"`
}

// ***************
// *** HELPERS ***
// ***************

// minCompatibleVersionAt is the floor a database at this version would carry
// having applied every step up to it in order. The gate reads the same figure
// off the recorded migration_log, so a downgraded database can sit at this
// version with a floor this walk never produces.
func minCompatibleVersionAt(registry []migrate.Migration, version int64) int64 {
	var floor int64
	for _, step := range registry {
		if step.Version > version {
			break
		}
		if step.MinCompatibleVersion > floor {
			floor = step.MinCompatibleVersion
		}
	}
	return floor
}
