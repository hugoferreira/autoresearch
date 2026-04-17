package store

import (
	"errors"
	"fmt"
)

// ErrInstrumentNotFound is returned by DeleteInstrument when the named
// instrument is not in config.yaml.
var ErrInstrumentNotFound = errors.New("instrument not found")

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

// DeleteInstrument removes the named instrument from config.yaml and
// returns the removed Instrument. Callers must check safety first via
// firewall.CheckInstrumentSafeToDelete; this method is purely the
// persistence half of the delete.
func (s *Store) DeleteInstrument(name string) (Instrument, error) {
	if name == "" {
		return Instrument{}, fmt.Errorf("instrument name is required")
	}
	cfg, err := s.Config()
	if err != nil {
		return Instrument{}, err
	}
	inst, ok := cfg.Instruments[name]
	if !ok {
		return Instrument{}, ErrInstrumentNotFound
	}
	delete(cfg.Instruments, name)
	if err := s.writeConfig(*cfg); err != nil {
		return Instrument{}, err
	}
	return inst, nil
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
