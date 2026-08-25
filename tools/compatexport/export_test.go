package main

import (
	"strings"
	"testing"

	"github.com/agentstax/vulkan/pkg/migrate"
)

// Both shipped registries are empty, so the real export is a single cell.
// This trail is what exercises the rule across versions: its one breaking
// step is what makes the rolling-deploy window visible closing and reopening.
var exampleRegistry = []migrate.Migration{
	{Version: 2, MinCompatibleVersion: 0},
	{Version: 3, MinCompatibleVersion: 0},
	{Version: 4, MinCompatibleVersion: 4},
	{Version: 5, MinCompatibleVersion: 0},
}

// The pre-v1 reality: no steps, so one version, and the only pair there is
// to ask about is admitted.
func TestNewExportEmptyRegistry(t *testing.T) {
	export, err := NewExport(nil, nil)
	if err != nil {
		t.Fatalf("NewExport(nil, nil) error = %v", err)
	}
	for name, scope := range map[string]*ScopeExport{"system": export.System, "topic": export.Topic} {
		if scope.Version != 1 {
			t.Errorf("%s version = %d, want 1", name, scope.Version)
		}
		if len(scope.Steps) != 0 {
			t.Errorf("%s steps = %d, want 0", name, len(scope.Steps))
		}
		if got := strings.Join(grid(scope), "|"); got != "s" {
			t.Errorf("%s grid = %q, want \"s\"", name, got)
		}
	}
}

// The whole grid the example trail produces, read as text: rows are database
// versions, columns are build versions. The diagonal is admitted, everything
// right of it wants the database migrated up, and the run of 's' left of it
// is the rolling-deploy window -- three wide at v3, slammed to one by the
// breaking step at v4, reopened from the new floor at v5.
func TestNewExportExampleGrid(t *testing.T) {
	want := []string{
		"soooo",
		"ssooo",
		"sssoo",
		"nnnso",
		"nnnss",
	}
	export, err := NewExport(exampleRegistry, exampleRegistry)
	if err != nil {
		t.Fatalf("NewExport(exampleRegistry) error = %v", err)
	}
	got := grid(export.Topic)
	if len(got) != len(want) {
		t.Fatalf("grid has %d rows, want %d", len(got), len(want))
	}
	for i, row := range want {
		if got[i] != row {
			t.Errorf("database v%d admits %q, want %q", i+1, got[i], row)
		}
	}
}

// The floor is the strictest declaration at or below the row's version, so
// the breaking step at v4 keeps binding at v5.
func TestNewExportFloors(t *testing.T) {
	want := []int64{0, 0, 0, 4, 4}
	export, err := NewExport(exampleRegistry, exampleRegistry)
	if err != nil {
		t.Fatalf("NewExport(exampleRegistry) error = %v", err)
	}
	for i, row := range export.Topic.Rows {
		if row.MinCompatibleVersion != want[i] {
			t.Errorf("database v%d floor = %d, want %d", row.Version, row.MinCompatibleVersion, want[i])
		}
	}
}

func TestNewExportSteps(t *testing.T) {
	export, err := NewExport(nil, exampleRegistry)
	if err != nil {
		t.Fatalf("NewExport(exampleRegistry) error = %v", err)
	}
	if len(export.Topic.Steps) != len(exampleRegistry) {
		t.Fatalf("steps = %d, want %d", len(export.Topic.Steps), len(exampleRegistry))
	}
	for i, step := range export.Topic.Steps {
		if step.Version != exampleRegistry[i].Version || step.MinCompatibleVersion != exampleRegistry[i].MinCompatibleVersion {
			t.Errorf("step %d = %+v, want %+v", i, step, exampleRegistry[i])
		}
	}
}

// A registry that fails migrate.Validate never reaches the grid, and the
// error names the scope that carried it.
func TestNewExportInvalidRegistry(t *testing.T) {
	broken := []migrate.Migration{{Version: 5}}
	if _, err := NewExport(broken, nil); err == nil || !strings.HasPrefix(err.Error(), "system: ") {
		t.Fatalf("NewExport(broken system) error = %v, want one prefixed \"system: \"", err)
	}
	if _, err := NewExport(nil, broken); err == nil || !strings.HasPrefix(err.Error(), "topic: ") {
		t.Fatalf("NewExport(broken topic) error = %v, want one prefixed \"topic: \"", err)
	}
}

// Every cell's build version must line up with its column, or the grid above
// is read against the wrong axis.
func TestNewExportCellAxes(t *testing.T) {
	export, err := NewExport(nil, exampleRegistry)
	if err != nil {
		t.Fatalf("NewExport(exampleRegistry) error = %v", err)
	}
	for i, row := range export.Topic.Rows {
		if row.Version != int64(i+1) {
			t.Errorf("row %d is version %d, want %d", i, row.Version, i+1)
		}
		for j, cell := range row.Cells {
			if cell.BuildVersion != int64(j+1) {
				t.Errorf("row %d cell %d is build %d, want %d", i, j, cell.BuildVersion, j+1)
			}
		}
	}
}

// ***************
// *** HELPERS ***
// ***************

// grid renders a scope as one string per database version so a test can
// state the whole matrix as text and a reader can see the regions.
func grid(scope *ScopeExport) []string {
	rows := make([]string, 0, len(scope.Rows))
	for _, row := range scope.Rows {
		line := strings.Builder{}
		for _, cell := range row.Cells {
			line.WriteString(symbol(cell.Support))
		}
		rows = append(rows, line.String())
	}
	return rows
}

func symbol(support migrate.SchemaSupport) string {
	switch support {
	case migrate.SchemaSupported:
		return "s"
	case migrate.SchemaOlderThanBuild:
		return "o"
	case migrate.SchemaNewerThanBuild:
		return "n"
	}
	return "?"
}
