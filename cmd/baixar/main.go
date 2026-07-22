// Comando baixar: etapa isolada de download. Dado {link, início, fim, id},
// baixa a legenda automática pt e o trecho da pregação, e gera a transcrição.
// Grava os artefatos em trabalho/<id>/ e persiste o pedido (pedido.json).
//
// Requer o yt-dlp instalado (ver README). Uso:
//
//	go run . -url "<link>" -inicio 00:05:30 -fim 00:38:10 -id teste
//
// Em falha, o pedido é salvo com status=erro e a mensagem em erro.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"srtclean/internal/download"
	"srtclean/internal/pipeline"
)

func main() {
	url := flag.String("url", "", "link do vídeo no YouTube (obrigatório)")
	inicio := flag.String("inicio", "", "início da pregação HH:MM:SS (obrigatório)")
	fim := flag.String("fim", "", "fim da pregação HH:MM:SS (obrigatório)")
	id := flag.String("id", "", "identificador do pedido (obrigatório)")
	base := flag.String("base", "trabalho", "pasta raiz de trabalho")
	bin := flag.String("bin", "yt-dlp", "binário do yt-dlp")
	sublang := flag.String("sublang", "pt", "idioma da legenda automática (ex.: pt, pt-orig)")
	formato := flag.String("format", "", "seletor de formato do yt-dlp (-f); vazio = melhor")
	flag.Parse()

	if *url == "" || *inicio == "" || *fim == "" || *id == "" {
		fmt.Fprintln(os.Stderr, "uso: baixar -url LINK -inicio HH:MM:SS -fim HH:MM:SS -id ID [-base trabalho] [-bin yt-dlp]")
		flag.PrintDefaults()
		os.Exit(2)
	}

	ped := pipeline.NovoPedido(*id, *url, *inicio, *fim, time.Now())

	b := &download.Baixador{Exec: download.ExecutorReal{}, Bin: *bin, BaseDir: *base, SubLangs: *sublang, Formato: *formato}
	err := b.Baixar(context.Background(), ped)

	// Persiste o pedido em qualquer caso (inclusive com status=erro).
	if salvarErr := ped.Salvar(*base); salvarErr != nil {
		fmt.Fprintf(os.Stderr, "aviso: não salvei o pedido: %v\n", salvarErr)
	}

	if err != nil {
		switch {
		case errors.Is(err, download.ErrSemLegenda):
			fmt.Fprintln(os.Stderr, "erro: vídeo não tem legenda automática pt. O processo para (não transcrevemos localmente).")
		case errors.Is(err, download.ErrVideoIndisponivel):
			fmt.Fprintln(os.Stderr, "erro: vídeo indisponível (privado, removido ou restrito).")
		case errors.Is(err, download.ErrTempoInvalido):
			fmt.Fprintln(os.Stderr, "erro: tempos inválidos. Use HH:MM:SS com fim maior que início.")
		default:
			fmt.Fprintf(os.Stderr, "erro no download: %v\n", err)
		}
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "ok: artefatos em %s/%s/ (legenda.srt, video.mp4, transcricao.txt)\n", *base, *id)
}
