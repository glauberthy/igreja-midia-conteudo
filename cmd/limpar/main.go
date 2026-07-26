// Comando limpar: apaga o material BRUTO dos pedidos antigos, preservando o histórico
// auditável (spec-06).
//
// Existe porque a fase pesada baixa o vídeo inteiro (~571 MB/pedido, medido): sem limpeza
// o disco enche. Mantém o bruto dos N pedidos mais recentes (padrão 1 — o suficiente para
// regerar um Short sem baixar de novo) e limpa os anteriores.
//
// NUNCA toca em finalizados/ (os Shorts entregues), nem em candidatos.corrigido.json,
// transcricao.txt, revisao-teologica.json ou pedido.json — ver internal/retencao.
//
// Uso:
//
//	go run ./cmd/limpar -dry-run          # mostra o que faria e quanto liberaria
//	go run ./cmd/limpar                   # limpa de verdade (retém o último pedido)
//	go run ./cmd/limpar -reter 3          # retém os 3 mais recentes
//	go run ./cmd/limpar -exceto web-...-1 # nunca tocar nesse pedido (ex.: em curso)
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"srtclean/internal/retencao"
)

func main() {
	base := flag.String("base", "trabalho", "pasta raiz de trabalho")
	reter := flag.Int("reter", 1, "quantos pedidos mais recentes manter intactos (mínimo 1)")
	exceto := flag.String("exceto", "", "id(s) que nunca podem ser tocados, separados por vírgula")
	dryRun := flag.Bool("dry-run", false, "só mostra o que seria apagado, sem apagar nada")
	verbose := flag.Bool("v", false, "lista os arquivos de cada pedido")
	flag.Parse()

	var intocaveis []string
	for _, id := range strings.Split(*exceto, ",") {
		if id = strings.TrimSpace(id); id != "" {
			intocaveis = append(intocaveis, id)
		}
	}

	res, err := retencao.Limpar(retencao.Opcoes{
		RaizTrabalho: *base,
		Reter:        *reter,
		Intocaveis:   intocaveis,
		DryRun:       *dryRun,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro na limpeza: %v\n", err)
		os.Exit(1)
	}

	if *dryRun {
		fmt.Println("== DRY-RUN: nada foi apagado ==")
	}
	for _, p := range res.Pedidos {
		fmt.Printf("%-34s %8s  (%d arquivo(s))\n", p.ID, retencao.FormatarBytes(p.Bytes), len(p.Arquivos))
		if *verbose {
			for _, a := range p.Arquivos {
				fmt.Printf("    - %s\n", a)
			}
		}
	}
	if len(res.Retidos) > 0 {
		fmt.Printf("retidos (intactos): %s\n", strings.Join(res.Retidos, ", "))
	}
	fmt.Println(res.Resumo())
}
