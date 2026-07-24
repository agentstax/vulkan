package datastore

// ListSystems yields the lone system entity
func (d *MigrateDatastore) ListSystems(entityId int64) ([]Entity, error) {
	return []Entity{{Id: entityId, Name: EntitySystem}}, nil
}
