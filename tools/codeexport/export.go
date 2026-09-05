package main

import (
	"fmt"
	"slices"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// Export is every VK-coded declaration keyed by its code. One document, so a
// page reads the record it needs by code and the drift check compares the
// whole registry in one pass.
type Export struct {
	Codes map[string]CodeRecord `json:"codes"`
}

// NewExport builds the export from the four registry listers. Passing them
// in rather than reading the registry directly is what lets a test declare
// its own.
func NewExport(errors []*diagnostic.DiagnosticError, events []*diagnostic.DiagnosticEvent, metrics []*diagnostic.DiagnosticMetric, alerts []*diagnostic.DiagnosticAlert) (*Export, error) {
	codes := make(map[string]CodeRecord, len(errors)+len(events)+len(metrics)+len(alerts))

	for _, declared := range errors {
		if err := putRecord(codes, newErrorRecord(declared)); err != nil {
			return nil, err
		}
	}
	for _, declared := range events {
		if err := putRecord(codes, newEventRecord(declared)); err != nil {
			return nil, err
		}
	}
	for _, declared := range metrics {
		if err := putRecord(codes, newMetricRecord(declared)); err != nil {
			return nil, err
		}
	}
	for _, declared := range alerts {
		if err := putRecord(codes, newAlertRecord(declared)); err != nil {
			return nil, err
		}
	}

	return &Export{Codes: codes}, nil
}

// CodeRecord is one declaration as the site reads it. The parts a kind does
// not have drop out, so a reader of the JSON sees only what was declared.
type CodeRecord struct {
	Code string `json:"code"`
	Kind string `json:"kind"` // error | event | metric | alert

	Problem         string   `json:"problem,omitempty"`          // error
	Recovery        string   `json:"recovery,omitempty"`         // error
	Fix             string   `json:"fix,omitempty"`              // error
	FixPlaceholders []string `json:"fix_placeholders,omitempty"` // error

	Message string `json:"message,omitempty"` // event

	Name          string   `json:"name,omitempty"`           // metric, alert
	MetricKind    string   `json:"metric_kind,omitempty"`    // metric
	Unit          string   `json:"unit,omitempty"`           // metric
	Description   string   `json:"description,omitempty"`    // metric, alert
	Scope         string   `json:"scope,omitempty"`          // metric, alert
	AttributeKeys []string `json:"attribute_keys,omitempty"` // metric
	Severity      string   `json:"severity,omitempty"`       // alert

	Queries []QueryRecord `json:"queries,omitempty"`
}

func newErrorRecord(declared *diagnostic.DiagnosticError) CodeRecord {
	return CodeRecord{
		Code:            declared.Code,
		Kind:            string(diagnostic.DiagnosticKindError),
		Problem:         declared.Problem,
		Recovery:        string(declared.Recovery),
		Fix:             declared.Fix,
		FixPlaceholders: declared.FixPlaceholders(),
		Queries:         newQueryRecords(declared.Queries),
	}
}

func newEventRecord(declared *diagnostic.DiagnosticEvent) CodeRecord {
	return CodeRecord{
		Code:    declared.Code,
		Kind:    string(diagnostic.DiagnosticKindEvent),
		Message: declared.Message,
		Queries: newQueryRecords(declared.Queries),
	}
}

func newMetricRecord(declared *diagnostic.DiagnosticMetric) CodeRecord {
	return CodeRecord{
		Code:          declared.Code,
		Kind:          string(diagnostic.DiagnosticKindMetric),
		Name:          declared.Name,
		MetricKind:    declared.Kind,
		Unit:          declared.Unit,
		Description:   declared.Description,
		Scope:         string(declared.Scope),
		AttributeKeys: slices.Clone(declared.AttributeKeys),
	}
}

func newAlertRecord(declared *diagnostic.DiagnosticAlert) CodeRecord {
	return CodeRecord{
		Code:        declared.Code,
		Kind:        string(diagnostic.DiagnosticKindAlert),
		Name:        declared.Name,
		Description: declared.Description,
		Scope:       string(declared.Scope),
		Severity:    declared.Severity,
	}
}

// QueryRecord is one diagnose query. The placeholders travel beside the SQL
// because the library already decides what a placeholder is -- a reader of
// this JSON never parses the SQL to find out.
type QueryRecord struct {
	Label        string   `json:"label"`
	Sql          string   `json:"sql"`
	Placeholders []string `json:"placeholders"`
}

func newQueryRecords(queries []*diagnostic.DiagnosticQuery) []QueryRecord {
	records := make([]QueryRecord, 0, len(queries))
	for _, query := range queries {
		records = append(records, QueryRecord{
			Label:        query.Label,
			Sql:          query.Sql,
			Placeholders: query.Placeholders(),
		})
	}
	return records
}

// ***************
// *** HELPERS ***
// ***************

// putRecord refuses a code two kinds both claim. The registry panics on that
// at init, so this catches only a caller that built its own lists -- but a
// map would drop one silently, and a dropped code is a page with no data.
func putRecord(codes map[string]CodeRecord, record CodeRecord) error {
	if existing, taken := codes[record.Code]; taken {
		return fmt.Errorf("%s is declared as both %s and %s", record.Code, existing.Kind, record.Kind)
	}
	codes[record.Code] = record
	return nil
}
