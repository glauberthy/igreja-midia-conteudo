// Comando harness: roda o harness de seleção em FASES (spec-07), mostrando a saída de
// cada fase — para inspecionar mapa, candidatos, delimitação de tempo, avaliação e os
// candidatos finais. O fluxo "de produção" é o cmd/selecionar (que chama o mesmo
// harness completo); este comando é para diagnóstico fase a fase.
//
// Uso (com o llama-server no ar):
//
//	go run . -transc trabalho/sermao/transcricao.txt              # fases 1→5
//	go run . -transc t.txt -ate 3                                 # só até a Fase 3
//	go run . -transc t.txt -prompt-dir prompts/ -out-final f.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"srtclean/internal/harness"
	"srtclean/internal/validacao"
)

func main() {
	transc := flag.String("transc", "", "transcrição limpa de entrada (.txt) — obrigatório")
	endpoint := flag.String("endpoint", harness.EndpointPadrao, "endpoint do llama-server")
	promptDir := flag.String("prompt-dir", "prompts", "pasta com fase1_mapa.md e fase2_candidatos.md")
	outMapa := flag.String("out-mapa", "", "opcional: salva o mapa (Fase 1) neste arquivo JSON")
	outCand := flag.String("out-cand", "", "opcional: salva os candidatos brutos (Fase 2) neste arquivo JSON")
	outDelim := flag.String("out-delim", "", "opcional: salva os candidatos delimitados (Fase 3) neste arquivo JSON")
	outFinal := flag.String("out-final", "", "opcional: salva os candidatos finais (Fase 5) neste arquivo JSON")
	declaracao := flag.String("declaracao", harness.DeclaracaoPadrao, "arquivo da Declaração Doutrinária (fase 4)")
	soMapa := flag.Bool("so-mapa", false, "roda apenas a Fase 1 (mapa)")
	ate := flag.Int("ate", 5, "roda até a fase N (1 a 5)")
	flag.Parse()

	if *transc == "" {
		fmt.Fprintln(os.Stderr, "uso: harness -transc transcricao.txt [-prompt-dir prompts/] [-out-mapa …] [-out-cand …] [-so-mapa]")
		flag.PrintDefaults()
		os.Exit(2)
	}

	transcricao, err := os.ReadFile(*transc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro ao ler transcrição: %v\n", err)
		os.Exit(1)
	}
	promptMapa := lerPrompt(*promptDir, "fase1_mapa.md")
	promptCand := lerPrompt(*promptDir, "fase2_candidatos.md")

	modelo := harness.NovoClienteLLM(*endpoint)
	ctx := context.Background()

	// --- Fase 1: Mapa ---
	fmt.Fprintln(os.Stderr, "Fase 1 (mapa do sermão)…")
	mapa, err := harness.Fase1Mapa(ctx, modelo, promptMapa, string(transcricao))
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro: %v\n", err)
		os.Exit(1)
	}
	imprimir("MAPA DO SERMÃO (Fase 1)", mapa)
	salvar(*outMapa, mapa)
	fmt.Fprintf(os.Stderr, "  tema + %d bloco(s) de ensino\n", len(mapa.Blocos))

	if *soMapa || *ate < 2 {
		return
	}

	// --- Fase 2: Candidatos (bloco + frase-âncora, sem tempo) ---
	fmt.Fprintln(os.Stderr, "Fase 2 (identificação de candidatos)…")
	cands, err := harness.Fase2Candidatos(ctx, modelo, promptCand, mapa, string(transcricao))
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro: %v\n", err)
		os.Exit(1)
	}
	imprimir("CANDIDATOS BRUTOS (Fase 2)", map[string]any{"candidatos": cands})
	salvar(*outCand, map[string]any{"candidatos": cands})
	fmt.Fprintf(os.Stderr, "  %d candidato(s) identificado(s) (sem tempo — isso é da Fase 3)\n", len(cands))

	if *ate < 3 {
		return
	}

	// --- Fase 3: Delimitação de tempo (100% código) ---
	fmt.Fprintln(os.Stderr, "Fase 3 (delimitação de tempo)…")
	delim, descartes := harness.Fase3Delimitar(cands, mapa, string(transcricao))
	imprimir("CANDIDATOS DELIMITADOS (Fase 3)", map[string]any{"candidatos": delim})
	salvar(*outDelim, map[string]any{"candidatos": delim})
	if len(descartes) > 0 {
		imprimir("DESCARTADOS na Fase 3 (inviáveis)", map[string]any{"descartados": descartes})
	}
	fmt.Fprintf(os.Stderr, "  %d viável(is) com 30–58s, %d descartado(s)\n", len(delim), len(descartes))

	if *ate < 4 {
		return
	}

	// --- Fase 4: Avaliação em duplicata (2 chamadas por candidato) ---
	fmt.Fprintln(os.Stderr, "Fase 4 (avaliação em duplicata)…")
	promptAval := lerPrompt(*promptDir, "fase4_avaliacao.md")
	if decl, err := os.ReadFile(*declaracao); err == nil {
		promptAval += "\n\n---\n\n# Declaração Doutrinária (parâmetro de fidelidade)\n\n" + string(decl)
	}
	var avaliados []validacao.Candidato
	for _, d := range delim {
		a1, err1 := harness.Fase4Avaliar(ctx, modelo, promptAval, d.Texto)
		a2, err2 := harness.Fase4Avaliar(ctx, modelo, promptAval, d.Texto)
		if err1 != nil || err2 != nil {
			fmt.Fprintf(os.Stderr, "  aviso: avaliação falhou para %q — pulado\n", resumo(d.Hook))
			continue
		}
		r := harness.CombinarAvaliacoes(a1, a2)
		marca := ""
		if r.RequerRevisao {
			marca = " [requer_revisao_reforcada]"
		}
		if r.Vetado {
			fmt.Fprintf(os.Stderr, "  VETADO (fidelidade) %q\n", resumo(d.Hook))
			continue
		}
		fmt.Fprintf(os.Stderr, "  score=%d%s %q\n", r.Score, marca, resumo(d.Hook))
		avaliados = append(avaliados, validacao.Candidato{
			Start: d.Start, End: d.End, DurationSeconds: d.DuracaoSegundos, Score: r.Score,
			Hook: d.Hook, Reason: r.Observacoes, CompleteThought: true,
			RequerRevisaoReforcada: r.RequerRevisao, Criteria: r.Criteria,
		})
	}

	if *ate < 5 {
		imprimir("CANDIDATOS AVALIADOS (Fase 4, antes da validação final)", map[string]any{"candidatos": avaliados})
		return
	}

	// --- Fase 5: Validação final (rede de segurança determinística) ---
	fmt.Fprintln(os.Stderr, "Fase 5 (validação final)…")
	finais, descFinais := harness.Fase5Validar(avaliados, string(transcricao))
	imprimir("CANDIDATOS FINAIS (Fase 5)", map[string]any{"candidatos": finais})
	salvar(*outFinal, map[string]any{"candidatos": finais})
	if len(descFinais) > 0 {
		imprimir("DESCARTADOS na Fase 5", map[string]any{"descartados": descFinais})
	}
	fmt.Fprintf(os.Stderr, "  %d candidato(s) final(is), %d descartado(s)\n", len(finais), len(descFinais))
}

func resumo(s string) string {
	if len(s) > 60 {
		return s[:60] + "…"
	}
	return s
}

func lerPrompt(dir, nome string) string {
	b, err := os.ReadFile(filepath.Join(dir, nome))
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro ao ler prompt %s: %v\n", nome, err)
		os.Exit(1)
	}
	return string(b)
}

func imprimir(titulo string, v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Printf("=== %s ===\n%s\n\n", titulo, string(b))
}

func salvar(path string, v any) {
	if path == "" {
		return
	}
	if dir := filepath.Dir(path); dir != "" {
		os.MkdirAll(dir, 0755)
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	if err := os.WriteFile(path, b, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "aviso: não salvei %q: %v\n", path, err)
	}
}
