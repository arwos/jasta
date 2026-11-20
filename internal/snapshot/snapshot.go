package snapshot

import (
	"encoding/json"
	"fmt"

	"go.arwos.org/jasta/internal/pkg/application"
	"go.arwos.org/jasta/internal/pkg/config"
)

const ProtocolVersion = 1

type Snapshot struct {
	Version  int    `json:"version"`
	Revision uint64 `json:"revision"`
	Sites    []Site `json:"sites"`
}

type Site struct {
	Type   config.ApplicationType `json:"type"`
	Config json.RawMessage        `json:"config"`
}

type Runtime struct {
	Revision uint64
	Apps     []application.Service
	Handlers map[string]application.Service
}

func Prepare(s Snapshot) (runtime *Runtime, err error) {
	if s.Version != ProtocolVersion {
		return nil, fmt.Errorf("unsupported snapshot version %d", s.Version)
	}
	if s.Revision == 0 {
		return nil, fmt.Errorf("snapshot revision must be greater than zero")
	}

	runtime = &Runtime{
		Revision: s.Revision,
		Apps:     make([]application.Service, 0, len(s.Sites)),
		Handlers: make(map[string]application.Service),
	}
	defer func() {
		if value := recover(); value != nil {
			runtime.Close()
			runtime = nil
			err = fmt.Errorf("prepare snapshot: %v", value)
		}
	}()

	for i, site := range s.Sites {
		appConfig, err := config.GetApplicationConfigByType(site.Type)
		if err != nil {
			runtime.Close()
			return nil, fmt.Errorf("site %d: %w", i, err)
		}
		if err = json.Unmarshal(site.Config, appConfig); err != nil {
			runtime.Close()
			return nil, fmt.Errorf("decode site %d config: %w", i, err)
		}
		if validator, ok := appConfig.(config.Validater); ok {
			if err = validator.Validate(); err != nil {
				runtime.Close()
				return nil, fmt.Errorf("validate site %d config: %w", i, err)
			}
		}

		service, err := application.Initialize(appConfig)
		if err != nil {
			runtime.Close()
			return nil, fmt.Errorf("initialize site %d: %w", i, err)
		}
		for _, domain := range service.Domains() {
			if _, exists := runtime.Handlers[domain]; exists {
				service.Close()
				runtime.Close()
				return nil, fmt.Errorf("duplicate domain %q in snapshot", domain)
			}
			runtime.Handlers[domain] = service
		}
		runtime.Apps = append(runtime.Apps, service)
	}

	return runtime, nil
}

func (r *Runtime) Close() {
	if r == nil {
		return
	}
	for _, app := range r.Apps {
		app.Close()
	}
	r.Apps = nil
	r.Handlers = nil
}
