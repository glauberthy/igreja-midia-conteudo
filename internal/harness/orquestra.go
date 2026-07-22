package harness

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"srtclean/internal/validacao"
)

// Padrões de localização dos prompts e da Declaração Doutrinária.
const (
	PromptDirPadrao  = "prompts"
	DeclaracaoPadrao = "Declaracao_Doutrinaria_da_Convencao_Batista_Brasil.md"
)

// Config parametriza a seleção multifase. Campos vazios recebem os padrões. Modelo
// pode ser injetado (testes com fake); se nil, usa o ClienteLLM sobre Endpoint.
type Config struct {
	Modelo         ModeloLLM
	Endpoint       string
	PromptDir      string
	DeclaracaoPath string
}

func (c Config) modelo() ModeloLLM {
	if c.Modelo != nil {
		return c.Modelo
	}
	return NovoClienteLLM(c.Endpoint)
}

// Selecionar roda o harness completo (Fases 1→5) sobre uma transcrição limpa e devolve
// os candidatos finais, já com tempo, score e marcações — todos validados. É a mesma
// assinatura externa da seleção antiga (o resto do sistema não muda), mas por dentro
// é o harness multifase (spec-07).
func Selecionar(ctx context.Context, transcricaoPath string, cfg Config) ([]validacao.Candidato, error) {
	transcBytes, err := os.ReadFile(transcricaoPath)
	if err != nil {
		return nil, fmt.Errorf("lendo transcrição %q: %w", transcricaoPath, err)
	}
	transc := string(transcBytes)

	dir := cfg.PromptDir
	if dir == "" {
		dir = PromptDirPadrao
	}
	promptMapa, err := os.ReadFile(filepath.Join(dir, "fase1_mapa.md"))
	if err != nil {
		return nil, fmt.Errorf("lendo prompt fase 1: %w", err)
	}
	promptCand, err := os.ReadFile(filepath.Join(dir, "fase2_candidatos.md"))
	if err != nil {
		return nil, fmt.Errorf("lendo prompt fase 2: %w", err)
	}
	promptAval, err := os.ReadFile(filepath.Join(dir, "fase4_avaliacao.md"))
	if err != nil {
		return nil, fmt.Errorf("lendo prompt fase 4: %w", err)
	}
	promptAvalCompleto := montarPromptAvaliacao(string(promptAval), cfg.DeclaracaoPath)

	modelo := cfg.modelo()

	// Fase 1 — Mapa.
	mapa, err := Fase1Mapa(ctx, modelo, string(promptMapa), transc)
	if err != nil {
		return nil, err
	}
	// Fase 2 — Candidatos (bloco + frase-âncora, sem tempo).
	brutos, err := Fase2Candidatos(ctx, modelo, string(promptCand), mapa, transc)
	if err != nil {
		return nil, err
	}
	// Fase 3 — Delimitação de tempo (código).
	delim, _ := Fase3Delimitar(brutos, mapa, transc)

	// Fase 4 — Avaliação em duplicata por candidato.
	var avaliados []validacao.Candidato
	for _, d := range delim {
		a1, err := Fase4Avaliar(ctx, modelo, promptAvalCompleto, d.Texto)
		if err != nil {
			continue // sem avaliação confiável, não entra
		}
		a2, err := Fase4Avaliar(ctx, modelo, promptAvalCompleto, d.Texto)
		if err != nil {
			continue
		}
		r := CombinarAvaliacoes(a1, a2)
		if r.Vetado {
			continue // reprovado por fidelidade
		}
		avaliados = append(avaliados, validacao.Candidato{
			Start:                  d.Start,
			End:                    d.End,
			DurationSeconds:        d.DuracaoSegundos,
			Score:                  r.Score,
			Hook:                   d.Hook,
			Reason:                 r.Observacoes,
			CompleteThought:        true,
			RequerRevisaoReforcada: r.RequerRevisao,
			Criteria:               r.Criteria,
		})
	}

	// Fase 5 — Validação final (rede de segurança determinística).
	finais, _ := Fase5Validar(avaliados, transc)
	return finais, nil
}

// montarPromptAvaliacao anexa a Declaração Doutrinária ao prompt da Fase 4. Se o
// caminho estiver vazio ou o arquivo não existir, usa só o prompt (não trava a seleção).
func montarPromptAvaliacao(prompt, declaracaoPath string) string {
	caminho := declaracaoPath
	if caminho == "" {
		caminho = DeclaracaoPadrao
	}
	decl, err := os.ReadFile(caminho)
	if err != nil {
		return prompt
	}
	return prompt + "\n\n---\n\n# Declaração Doutrinária (parâmetro de fidelidade)\n\n" + string(decl)
}
