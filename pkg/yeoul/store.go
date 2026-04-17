package yeoul

type StorageDriver string

const (
	StorageDriverLadybug StorageDriver = "ladybug"
)

type stateStore interface {
	Load() (*persistedState, error)
	Save(state persistedState) error
	Close() error
}

type persistedState struct {
	Version  int                `json:"version"`
	Sequence uint64             `json:"sequence"`
	Sources  map[string]Source  `json:"sources"`
	Episodes map[string]Episode `json:"episodes"`
	Entities map[string]Entity  `json:"entities"`
	Facts    map[string]Fact    `json:"facts"`
}

func openStateStore(cfg Config) (stateStore, error) {
	if cfg.InMemory {
		return memoryStore{}, nil
	}

	switch resolveStorageDriver(cfg) {
	case StorageDriverLadybug:
		return newLadybugStore(cfg)
	default:
		return nil, errorf(ErrConfigInvalid, "unsupported storage driver", map[string]any{
			"driver": cfg.Driver,
		}, nil)
	}
}

func resolveStorageDriver(cfg Config) StorageDriver {
	if cfg.Driver != "" {
		return cfg.Driver
	}
	return StorageDriverLadybug
}

func emptyPersistedState() persistedState {
	return persistedState{
		Version:  1,
		Sources:  make(map[string]Source),
		Episodes: make(map[string]Episode),
		Entities: make(map[string]Entity),
		Facts:    make(map[string]Fact),
	}
}

type memoryStore struct{}

func (memoryStore) Load() (*persistedState, error) {
	state := emptyPersistedState()
	return &state, nil
}

func (memoryStore) Save(state persistedState) error {
	_ = state
	return nil
}

func (memoryStore) Close() error {
	return nil
}

func defaultSources(in map[string]Source) map[string]Source {
	if in != nil {
		return in
	}
	return make(map[string]Source)
}

func defaultEpisodes(in map[string]Episode) map[string]Episode {
	if in != nil {
		return in
	}
	return make(map[string]Episode)
}

func defaultEntities(in map[string]Entity) map[string]Entity {
	if in != nil {
		return in
	}
	return make(map[string]Entity)
}

func defaultFacts(in map[string]Fact) map[string]Fact {
	if in != nil {
		return in
	}
	return make(map[string]Fact)
}
