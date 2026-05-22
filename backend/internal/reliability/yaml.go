// YAMLProvider lê SLOs de arquivos YAML locais no estilo Google SRE
// Workbook. Útil para times que ainda não têm provider externo mas
// querem declarar objetivos versionados em git.
//
// Formato esperado por arquivo:
//
//   slos:
//     - id: api-availability
//       name: \"API availability\"
//       service: dora-api
//       target: 99.9
//       periodDays: 30
//       indicators:
//         - actual: 99.87        # valor MEDIDO atual (preenchido manualmente
//                                #  ou via job externo que reescreve o YAML)
//
// O actual é deliberadamente declarativo (não calculamos a partir do
// arquivo). Combina bem com pipelines que rodam o cálculo no CI e
// commitam o YAML atualizado — auditoria clara via git blame.
//
// Diretório lido: env RELIABILITY_YAML_DIR (default "/etc/dora/slos").
package reliability

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// YAMLProvider implementa Provider lendo arquivos *.yaml de um diretório.
type YAMLProvider struct {
	dir string
}

// NewYAMLProvider lê env RELIABILITY_YAML_DIR.
func NewYAMLProvider() (*YAMLProvider, error) {
	dir := os.Getenv("RELIABILITY_YAML_DIR")
	if dir == "" {
		dir = "/etc/dora/slos"
	}
	return &YAMLProvider{dir: dir}, nil
}

func (y *YAMLProvider) Name() string { return "yaml" }

type yamlDoc struct {
	SLOs []struct {
		ID         string  `yaml:"id"`
		Name       string  `yaml:"name"`
		Service    string  `yaml:"service"`
		Target     float64 `yaml:"target"`
		PeriodDays int     `yaml:"periodDays"`
		Indicators []struct {
			Actual float64 `yaml:"actual"`
		} `yaml:"indicators"`
	} `yaml:"slos"`
}

// ListSLOs lê todos os .yaml do diretório configurado. scopeRef filtra
// por service.
func (y *YAMLProvider) ListSLOs(_ context.Context, scopeRef string) ([]SLOStatus, error) {
	entries, err := os.ReadDir(y.dir)
	if err != nil {
		// Diretório ausente = nenhum SLO declarado. Não é erro.
		if os.IsNotExist(err) {
			return []SLOStatus{}, nil
		}
		return nil, fmt.Errorf("read yaml dir %q: %w", y.dir, err)
	}

	var out []SLOStatus
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") && !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(y.dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		var doc yamlDoc
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		for _, s := range doc.SLOs {
			if scopeRef != "" && s.Service != scopeRef {
				continue
			}
			period := s.PeriodDays
			if period == 0 {
				period = 30
			}
			var actual float64
			if len(s.Indicators) > 0 {
				actual = s.Indicators[0].Actual
			}

			var consumed float64
			denom := 100 - s.Target
			if denom > 0 {
				consumed = (s.Target - actual) / denom
			}
			if consumed < 0 {
				consumed = 0
			}
			if consumed > 1 {
				consumed = 1
			}
			out = append(out, SLOStatus{
				ID:          s.ID,
				Name:        s.Name,
				Target:      s.Target,
				Actual:      actual,
				ErrorBudget: consumed,
				PeriodDays:  period,
				Status:      classifyStatus(consumed),
				Source:      "yaml",
			})
		}
	}
	return out, nil
}
