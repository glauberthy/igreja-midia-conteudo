// Comando selecionar: fluxo ponta a ponta da SELEÇÃO (sem vídeo).
// Recebe uma transcrição já limpa, chama o modelo (llama-server) e aplica a
// correção determinística, imprimindo/salvando os candidatos já corrigidos.
//
// Uso (com o llama-server de pé):
//
//	go run . -transc transcricao_1.txt -out trabalho/teste/candidatos.json
//	go run . -transc transcricao_1.txt                 # imprime no stdout
//	go run . -transc t.txt -endpoint http://host:8080/v1/chat/completions
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

	"srtclean/internal/pipeline"
)

func main() {
	transc := flag.String("transc", "", "transcrição limpa de entrada (.txt) — obrigatório")
	out := flag.String("out", "", "arquivo de saída dos candidatos corrigidos (padrão: stdout)")
	endpoint := flag.String("endpoint", pipeline.EndpointPadrao, "endpoint do llama-server")
	prompt := flag.String("prompt", pipeline.PromptPadrao, "prompt de sistema da seleção")
	flag.Parse()

	if *transc == "" {
		fmt.Fprintln(os.Stderr, "uso: selecionar -transc transcricao.txt [-out candidatos.json] [-endpoint URL] [-prompt caminho]")
		flag.PrintDefaults()
		os.Exit(2)
	}

	cfg := pipeline.Config{Endpoint: *endpoint, PromptPath: *prompt}
	candidatos, err := pipeline.Selecionar(context.Background(), *transc, cfg)
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
		fmt.Fprintf(os.Stderr, "ok: %d candidato(s) corrigido(s) gravado(s) em %s\n", len(candidatos), *out)
	}
}
