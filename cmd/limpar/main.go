// Comando limpar: apaga o material BRUTO dos pedidos antigos, preservando o histórico
// auditável (spec-06).
//
// Existe porque a fase pesada baixa o vídeo inteiro (~571 MB, medido): sem limpeza o disco
// enche. Mantém o bruto dos N pedidos mais recentes (padrão 1) e limpa os anteriores.
//
// DUAS políticas, porque há dois níveis de armazenamento (spec-05 v3):
//
//	trabalho/<idPedido>/   por CONTAGEM: mantém os N pedidos mais recentes (agora são KB)
//	videos/<idDoVídeo>/    por PRAZO E TETO: 30 dias desde o último uso, 50 GB de teto
//
// O arquivo grande (~570 MB) mora no cache desde o cache por vídeo, então é a segunda política
// que libera disco. Ela apaga só o que é regenerável (video.mp4, legenda.info.json) e preserva a
// legenda e a transcrição do culto — 400 KB que custam 3 s e uma requisição ao YouTube.
//
// NUNCA toca em finalizados/ (os Shorts entregues), nem em candidatos.corrigido.json,
// transcricao.txt, revisao-teologica.json ou pedido.json — ver internal/retencao.
//
// Uso:
//
//	go run ./cmd/limpar -dry-run          # mostra o que faria e quanto liberaria
//	go run ./cmd/limpar                   # limpa de verdade (retém o último pedido)
//	go run ./cmd/limpar -reter 3          # retém os 3 mais recentes
//	go run ./cmd/limpar -exceto web-...-1 # nunca tocar nesse pedido (nem no vídeo dele)
//	go run ./cmd/limpar -video-dias 7     # expira culto sem uso há mais de 7 dias
//	go run ./cmd/limpar -video-teto 10    # teto de 10 GB no cache de vídeos
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"srtclean/internal/pipeline"
	"srtclean/internal/retencao"
	"srtclean/internal/videocache"
)

func main() {
	base := flag.String("base", "trabalho", "pasta raiz de trabalho")
	videos := flag.String("videos", videocache.DirPadrao, "raiz do cache por vídeo")
	videoDias := flag.Int("video-dias", videocache.DiasPadrao, "expira o culto sem uso há mais dias "+
		"que isto (idade pelo ÚLTIMO USO, não pelo download)")
	videoTetoGB := flag.Int("video-teto", int(videocache.TetoPadrao>>30), "teto do cache de vídeos, "+
		"em GB (mínimo 1): acima disso expira do uso mais antigo para o mais novo até caber")
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
	// Mínimo 1: no contrato do pacote, zero significa "use o padrão" — então `-video-teto 0`,
	// digitado por quem quer esvaziar o cache, cairia justamente nos 50 GB. Aqui o valor da flag
	// é sempre o que manda.
	expirarCache(*videos, *base, max(*videoDias, 1), int64(max(*videoTetoGB, 1))<<30,
		intocaveis, *dryRun, *verbose)
}

// expirarCache aplica a política do cache por vídeo (prazo + teto).
//
// Os intocáveis chegam como ids de PEDIDO (a flag -exceto), e aqui viram ids de VÍDEO lendo o
// pedido.json de cada um. É derivado, e não uma segunda flag, porque o operador não sabe de cor
// o id do YouTube do culto em curso — pedir isso à mão seria pedir para errar justamente na
// proteção que evita apagar o vídeo de um render em andamento.
func expirarCache(dirVideos, base string, dias int, teto int64, pedidosIntocaveis []string, dryRun, verbose bool) {
	c := videocache.Novo(dirVideos)
	var videos []string
	for _, id := range pedidosIntocaveis {
		ped, err := pipeline.Carregar(base, id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "aviso: não li %s para descobrir de qual vídeo ele é (%v): o "+
				"vídeo dele NÃO está protegido nesta passagem\n", id, err)
			continue
		}
		if ped.VideoID != "" {
			videos = append(videos, ped.VideoID)
		}
	}

	res, err := c.Expirar(videocache.OpcoesExpiracao{
		Dias: dias, Teto: teto, Intocaveis: videos, DryRun: dryRun,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro na expiração do cache: %v\n", err)
		os.Exit(1)
	}
	if len(res.Cultos) == 0 && len(res.Retidos) == 0 && len(res.EmUso) == 0 {
		return // cache vazio: nada a dizer
	}
	fmt.Println()
	for _, cu := range res.Cultos {
		fmt.Printf("%-34s %8s  (%s; último uso %s)\n", cu.VideoID, retencao.FormatarBytes(cu.Bytes),
			cu.Motivo, cu.UsadoEm.Format("2006-01-02"))
		if verbose {
			for _, a := range cu.Arquivos {
				fmt.Printf("    - %s\n", a)
			}
		}
	}
	if len(res.EmUso) > 0 {
		fmt.Printf("em uso (intocáveis): %s\n", strings.Join(res.EmUso, ", "))
	}
	fmt.Println(res.Resumo())
}
