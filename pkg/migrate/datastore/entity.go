package datastore

// Entity types -- the schema_log.entity_type a step migrates under.
const (
	EntitySystem = "system"
	EntityTopic  = "topic"
)

// Entity is one thing to migrate:
// - system has a single id-0 entity
// - topic one per topic row.
//
// Name labels the entity for error handling
type Entity struct {
	Id   int64
	Name string
}
