package planner

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/selene-linux/selene/internal/catalog"
)

// Environment contains only the host values needed to produce a plan.
type Environment struct {
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	Home          string `json:"home"`
	XDGDataHome   string `json:"xdg_data_home"`
	XDGCacheHome  string `json:"xdg_cache_home"`
	XDGConfigHome string `json:"xdg_config_home"`
	XDGStateHome  string `json:"xdg_state_home"`
}

// Operation is one explicit, reviewable action in an installation plan.
type Operation struct {
	Order     int    `json:"order"`
	Phase     string `json:"phase"`
	Component string `json:"component,omitempty"`
	Action    string `json:"action"`
	Target    string `json:"target,omitempty"`
	Risk      string `json:"risk"`
}

// Plan describes an installation without applying it.
type Plan struct {
	CatalogRevision string      `json:"catalog_revision"`
	BundleID        string      `json:"bundle_id"`
	BundleName      string      `json:"bundle_name"`
	Environment     Environment `json:"environment"`
	Ready           bool        `json:"ready"`
	Blockers        []string    `json:"blockers,omitempty"`
	Operations      []Operation `json:"operations"`
}

// DetectEnvironment resolves XDG locations without creating them.
func DetectEnvironment() (Environment, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Environment{}, fmt.Errorf("locate home directory: %w", err)
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		cacheHome = filepath.Join(home, ".cache")
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		stateHome = filepath.Join(home, ".local", "state")
	}
	return Environment{
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		Home:          filepath.Clean(home),
		XDGDataHome:   filepath.Clean(dataHome),
		XDGCacheHome:  filepath.Clean(cacheHome),
		XDGConfigHome: filepath.Clean(configHome),
		XDGStateHome:  filepath.Clean(stateHome),
	}, nil
}

// Build creates a deterministic, read-only installation plan.
func Build(source catalog.Catalog, bundleID string, env Environment) (Plan, error) {
	bundle, ok := source.Bundle(bundleID)
	if !ok {
		return Plan{}, fmt.Errorf("bundle %q not found", bundleID)
	}
	components, err := source.OrderedComponents(bundle)
	if err != nil {
		return Plan{}, err
	}

	plan := Plan{
		CatalogRevision: source.Revision,
		BundleID:        bundle.ID,
		BundleName:      bundle.Name,
		Environment:     env,
		Ready:           true,
	}
	if env.OS != "linux" {
		plan.Blockers = append(plan.Blockers, fmt.Sprintf("plataforma %s não suportada; o instalador tem como alvo Linux", env.OS))
	}
	if env.Arch != "amd64" {
		plan.Blockers = append(plan.Blockers, fmt.Sprintf("arquitetura %s não suportada; os artefatos atuais exigem amd64/x86_64", env.Arch))
	}

	add := func(operation Operation) {
		operation.Order = len(plan.Operations) + 1
		plan.Operations = append(plan.Operations, operation)
	}
	add(Operation{
		Phase: "preflight", Action: "Confirmar que Steam e Lumen podem ser encerrados antes da instalação", Risk: "medium",
	})
	add(Operation{
		Phase: "snapshot", Action: "Salvar arquivos afetados e abrir um journal persistente para rollback",
		Target: filepath.Join(env.XDGStateHome, "selene", "transactions"), Risk: "medium",
	})

	for _, component := range components {
		destination, err := expandDestination(component.Install.Destination, env)
		if err != nil {
			return Plan{}, fmt.Errorf("component %s: %w", component.ID, err)
		}
		cacheTarget := filepath.Join(env.XDGCacheHome, "selene", "downloads", component.Artifact.SHA256+"-"+component.Artifact.Name)

		add(Operation{
			Phase: "download", Component: component.ID,
			Action: fmt.Sprintf("Baixar %s %s por HTTPS", component.Name, component.Version),
			Target: cacheTarget, Risk: "low",
		})
		add(Operation{
			Phase: "verify", Component: component.ID,
			Action: fmt.Sprintf("Verificar tamanho %d e SHA-256 %s", component.Artifact.Size, shortHash(component.Artifact.SHA256)),
			Target: cacheTarget, Risk: "low",
		})
		add(Operation{
			Phase: "stage", Component: component.ID,
			Action: fmt.Sprintf("Validar caminhos e conteúdo obrigatório do artefato %s", component.Artifact.Format),
			Target: destination, Risk: "low",
		})
		add(Operation{
			Phase: "backup", Component: component.ID,
			Action: "Criar snapshot da instalação existente antes de substituí-la",
			Target: destination, Risk: "medium",
		})
		if len(component.Install.Preserve) > 0 {
			add(Operation{
				Phase: "preserve", Component: component.ID,
				Action: "Preservar dados do usuário: " + strings.Join(component.Install.Preserve, ", "),
				Target: destination, Risk: "medium",
			})
		}

		if component.Install.Strategy == "verified-script" {
			add(Operation{
				Phase: "configure", Component: component.ID,
				Action: "Executar o setup.sh fixado e verificado com sudo bloqueado e escopo somente do usuário",
				Target: destination, Risk: "high",
			})
		} else {
			add(Operation{
				Phase: "activate", Component: component.ID,
				Action: "Ativar a árvore preparada por troca atômica e registrar rollback",
				Target: destination, Risk: "medium",
			})
		}
	}

	add(Operation{
		Phase: "verify", Action: "Validar os arquivos instalados; em qualquer falha, parar serviços e restaurar o snapshot", Risk: "medium",
	})
	plan.Ready = len(plan.Blockers) == 0
	return plan, nil
}

func expandDestination(value string, env Environment) (string, error) {
	var base, token string
	switch {
	case strings.HasPrefix(value, "${XDG_DATA_HOME}/"):
		base, token = env.XDGDataHome, "${XDG_DATA_HOME}"
	case strings.HasPrefix(value, "${HOME}/"):
		base, token = env.Home, "${HOME}"
	default:
		return "", fmt.Errorf("destination %q is outside HOME and XDG_DATA_HOME", value)
	}
	relative := strings.TrimPrefix(value, token+"/")
	if relative == "" {
		return "", fmt.Errorf("destination is empty")
	}
	parts := strings.Split(relative, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("destination contains unsafe path segment")
		}
	}
	return filepath.Join(append([]string{base}, parts...)...), nil
}

func shortHash(value string) string {
	if len(value) <= 16 {
		return value
	}
	return value[:12] + "…" + value[len(value)-4:]
}
