package migrate

// Version is the schema version a registry defines: the v1 baseline plus one
// per step. Derived so it can never drift from the registry.
func Version(registry []Migration) int64 {
	return int64(len(registry)) + 1
}

// SchemaSupport is the gate's conclusion about one (schema version, build
// version) pair -- the classification behind ErrSchemaOlderThanBuild and
// ErrSchemaNewerThanBuild.
type SchemaSupport string

const (
	SchemaSupported      SchemaSupport = "supported"
	SchemaOlderThanBuild SchemaSupport = "older_than_build"
	SchemaNewerThanBuild SchemaSupport = "newer_than_build"
)

// ClassifySchemaSupport is the gate's rule: supported iff
// minCompatibleVersion <= buildVersion <= version. Additive steps declare
// minCompatibleVersion 0, so a schema migrated past the build by additive
// steps alone stays supported -- that window is what makes a rolling deploy
// safe.
func ClassifySchemaSupport(version int64, minCompatibleVersion int64, buildVersion int64) SchemaSupport {
	switch {
	case version < buildVersion:
		return SchemaOlderThanBuild
	case minCompatibleVersion > buildVersion:
		return SchemaNewerThanBuild
	}
	return SchemaSupported
}
