// Comando selecionar: seleção de candidatos a Short a partir de uma transcrição limpa.
// Roda o HARNESS MULTIFASE (spec-07): mapa → candidatos → delimitação de tempo →
// avaliação em duplicata → validação final. Imprime/salva os candidatos finais.
//
// Uso (com o llama-server de pé):
//
//	go run . -transc trabalho/sermao/transcricao.txt -out trabalho/sermao/candidatos.corrigido.json
//	go run . -transc t.txt -prompt-dir prompts/ -endpoint http://host:8080/v1/chat/completions
//
// A chave de API (modo externo) vem só da variável de ambiente LLM_API_KEY.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"srtclean/internal/harness"
)

func main() {
	transc := flag.String("transc", "", "transcrição limpa de entrada (.txt) — obrigatório")
	out := flag.String("out", "", "arquivo de saída dos candidatos finais (padrão: stdout)")
	endpoint := flag.String("endpoint", harness.EndpointPadrao, "endpoint do llama-server")
	promptDir := flag.String("prompt-dir", harness.PromptDirPadrao, "pasta com os prompts das fases")
	declaracao := flag.String("declaracao", harness.DeclaracaoPadrao, "arquivo da Declaração Doutrinária (fase de avaliação)")
	flag.Parse()

	if *transc == "" {
		fmt.Fprintln(os.Stderr, "uso: selecionar -transc transcricao.txt [-out candidatos.json] [-prompt-dir prompts/] [-endpoint URL]")
		flag.PrintDefaults()
		os.Exit(2)
	}

	cfg := harness.Config{Endpoint: *endpoint, PromptDir: *promptDir, DeclaracaoPath: *declaracao}
	candidatos, err := harness.Selecionar(context.Background(), *transc, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro na seleção: %v\n", err)
		os.Exit(1)
	}

	doc := map[string]any{"candidatos": candidatos}
	saida, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro ao serializar: %v\n", err)
		os.Exit(1)
	}

	if *out == "" {
		fmt.Println(string(saida))
	} else {
		if err := os.MkdirAll(filepath.Dir(*out), 0755); err != nil {
			fmt.Fprintf(os.Stderr, "erro ao criar pasta de saída: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*out, saida, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "erro ao gravar %q: %v\n", *out, err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "ok: %d candidato(s) final(is) gravado(s) em %s\n", len(candidatos), *out)
	}
}
