package topic

type Topic struct {
	Id   int64
	Name string
}

func NewTopic(name string, datastore Datastore) *Topic {
	return &Topic{
		Name: name,
	}
}

func (t *Topic) Create() error {
	return nil
}
