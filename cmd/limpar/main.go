// Comando limpar: apaga o material BRUTO dos pedidos antigos, preservando o histórico
// auditável (spec-06).
//
// Existe porque a fase pesada baixa o vídeo inteiro (~571 MB, medido): sem limpeza o disco
// enche. Mantém o bruto dos N pedidos mais recentes (padrão 1) e limpa os anteriores.
//
// ATENÇÃO — desde o cache por vídeo (spec-05 v3) o arquivo grande NÃO está mais em
// trabalho/<idPedido>/: está em videos/<idDoVídeo>/, que esta limpeza não enxerga. Então aqui
// se libera pouco, e é esperado. O comando avisa no fim, com o tamanho do cache, para ninguém
// concluir que a limpeza quebrou. A expiração do cache é a Parte 3 da spec-05 v3.
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
	"path/filepath"
	"strings"

	"srtclean/internal/retencao"
	"srtclean/internal/videocache"
)

func main() {
	base := flag.String("base", "trabalho", "pasta raiz de trabalho")
	videos := flag.String("videos", videocache.DirPadrao, "raiz do cache por vídeo (só para o "+
		"aviso: esta limpeza NÃO apaga o cache)")
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
	avisarSobreOCache(*videos)
}

// avisarSobreOCache explica por que este comando pode liberar quase nada.
//
// Desde o cache por vídeo (spec-05 v3), o arquivo grande NÃO mora mais em trabalho/<id>/ —
// está em videos/<idDoVídeo>/, que esta limpeza não enxerga (de propósito: apagar o cache é o
// oposto do que ele existe para fazer). Quem rodar esperando ~570 MB de volta receberia "0 B
// liberados" sem nenhuma explicação, e concluiria que a limpeza quebrou.
//
// É um aviso, não uma política: a expiração do cache (prazo + teto) é a Parte 3 da spec-05 v3.
// Aviso resolve até lá, e o texto diz o tamanho do que está fora do alcance — que é a
// informação que o operador precisa para decidir apagar à mão se o disco apertar.
func avisarSobreOCache(dirVideos string) {
	bytes, dirs := tamanhoDoCache(dirVideos)
	if dirs == 0 {
		return
	}
	fmt.Printf("\nnota: o cache de vídeos NÃO é tocado por esta limpeza — %s em %d culto(s) em %s/.\n"+
		"      É onde o arquivo grande mora desde o cache por vídeo, e ele serve os próximos\n"+
		"      pedidos do mesmo culto. A expiração automática (prazo + teto) é a Parte 3 da\n"+
		"      spec-05 v3; até lá, se o disco apertar, apague à mão a pasta do culto que não\n"+
		"      vai mais ser usado.\n", retencao.FormatarBytes(bytes), dirs, dirVideos)
}

// tamanhoDoCache soma os bytes e conta os cultos em videos/. Percorrer o diretório é suficiente
// e evita depender do video.json (que pode faltar num cache migrado à mão).
func tamanhoDoCache(dirVideos string) (int64, int) {
	entradas, err := os.ReadDir(dirVideos)
	if err != nil {
		return 0, 0
	}
	var total int64
	cultos := 0
	for _, e := range entradas {
		if !e.IsDir() {
			continue
		}
		cultos++
		arqs, err := os.ReadDir(filepath.Join(dirVideos, e.Name()))
		if err != nil {
			continue
		}
		for _, a := range arqs {
			if fi, err := a.Info(); err == nil {
				total += fi.Size()
			}
		}
	}
	return total, cultos
}
