package datastore

// ListSystems yields the lone system entity
func (d *MigrateDatastore) ListSystems() ([]Entity, error) {
	return []Entity{{Id: 0, Name: EntitySystem}}, nil
}
