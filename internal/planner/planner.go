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
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	Home         string `json:"home"`
	XDGDataHome  string `json:"xdg_data_home"`
	XDGCacheHome string `json:"xdg_cache_home"`
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
	return Environment{
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		Home:         filepath.Clean(home),
		XDGDataHome:  filepath.Clean(dataHome),
		XDGCacheHome: filepath.Clean(cacheHome),
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
		add(Operation{
			Phase: "activate", Component: component.ID,
			Action: "Ativar a árvore preparada por troca atômica e registrar rollback",
			Target: destination, Risk: "medium",
		})

		if component.Install.Strategy == "native-adapter" {
			plan.Blockers = append(plan.Blockers,
				"o adaptador Go do slsteam-moon ainda precisa reproduzir com segurança o wrapper LD_AUDIT, entradas desktop e serviços do usuário")
			add(Operation{
				Phase: "configure", Component: component.ID,
				Action: "Configurar integração da Steam usando o adaptador nativo do Selene (pendente)",
				Target: destination, Risk: "high",
			})
		}
	}

	add(Operation{
		Phase: "verify", Action: "Executar diagnóstico pós-instalação e restaurar o snapshot se houver falha", Risk: "medium",
	})
	plan.Ready = len(plan.Blockers) == 0
	return plan, nil
}

func expandDestination(value string, env Environment) (string, error) {
	const token = "${XDG_DATA_HOME}"
	if !strings.HasPrefix(value, token+"/") {
		return "", fmt.Errorf("destination %q is outside XDG_DATA_HOME", value)
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
	return filepath.Join(append([]string{env.XDGDataHome}, parts...)...), nil
}

func shortHash(value string) string {
	if len(value) <= 16 {
		return value
	}
	return value[:12] + "…" + value[len(value)-4:]
}
