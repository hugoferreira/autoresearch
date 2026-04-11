package store

import "fmt"

func (s *Store) RegisterInstrument(name string, inst Instrument) error {
	if name == "" {
		return fmt.Errorf("instrument name is required")
	}
	cfg, err := s.Config()
	if err != nil {
		return err
	}
	if cfg.Instruments == nil {
		cfg.Instruments = map[string]Instrument{}
	}
	cfg.Instruments[name] = inst
	return s.writeConfig(*cfg)
}

func (s *Store) ListInstruments() (map[string]Instrument, error) {
	cfg, err := s.Config()
	if err != nil {
		return nil, err
	}
	if cfg.Instruments == nil {
		return map[string]Instrument{}, nil
	}
	return cfg.Instruments, nil
}
