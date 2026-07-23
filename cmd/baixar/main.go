// Comando baixar: etapa isolada de download. Dado {link, início, fim, id},
// baixa a legenda automática pt e o trecho da pregação, e gera a transcrição.
// Grava os artefatos em trabalho/<id>/ e persiste o pedido (pedido.json).
//
// Cada sermão deve ter um -id próprio (ex.: -id sermao-<slug>). Reutilizar um id que
// já aponta para OUTRO vídeo/janela é recusado, para nunca misturar o vídeo de um
// pedido com a transcrição de outro; use outro -id ou -force para substituir.
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
	"path/filepath"
	"time"

	"srtclean/internal/download"
	"srtclean/internal/pipeline"
)

// decisao expressa o que fazer ao baixar um id que talvez já exista na pasta de trabalho.
type decisao int

const (
	baixarNormal    decisao = iota // não há pedido anterior, ou é o mesmo pedido: baixa
	limparERebaixar                // troca de vídeo, ou -force: limpa o dir antes de baixar
	recusarConflito                // id já aponta para outro vídeo e sem -force: recusa
)

// decidirDownload decide a ação dado o pedido já existente neste id (nil se não há), o
// pedido novo e a flag -force. É pura (sem I/O) para ficar testável. A regra central:
// nunca deixar o vídeo de um pedido conviver com a transcrição de outro no mesmo id.
func decidirDownload(existente, novo *pipeline.Pedido, force bool) decisao {
	if existente == nil {
		return baixarNormal
	}
	mesmo := existente.YouTubeURL == novo.YouTubeURL &&
		existente.Inicio == novo.Inicio &&
		existente.Fim == novo.Fim
	switch {
	case !mesmo && !force:
		return recusarConflito // id já é de outro vídeo/janela: exige -id novo ou -force
	case !mesmo, force:
		return limparERebaixar // troca de vídeo, ou rebaixa forçado: começa do zero
	default:
		return baixarNormal // mesmo pedido: refaz o download (idempotente)
	}
}

func main() {
	url := flag.String("url", "", "link do vídeo no YouTube (obrigatório)")
	inicio := flag.String("inicio", "", "início da pregação HH:MM:SS (obrigatório)")
	fim := flag.String("fim", "", "fim da pregação HH:MM:SS (obrigatório)")
	id := flag.String("id", "", "identificador do pedido (obrigatório)")
	base := flag.String("base", "trabalho", "pasta raiz de trabalho")
	bin := flag.String("bin", "yt-dlp", "binário do yt-dlp")
	sublang := flag.String("sublang", "pt", "idioma da legenda automática (ex.: pt, pt-orig)")
	formato := flag.String("format", "", "seletor de formato do yt-dlp (-f); vazio = melhor")
	force := flag.Bool("force", false, "rebaixa mesmo se o id já existir; substitui um id que aponte para outro vídeo")
	flag.Parse()

	if *url == "" || *inicio == "" || *fim == "" || *id == "" {
		fmt.Fprintln(os.Stderr, "uso: baixar -url LINK -inicio HH:MM:SS -fim HH:MM:SS -id ID [-base trabalho] [-bin yt-dlp] [-force]")
		flag.PrintDefaults()
		os.Exit(2)
	}

	ped := pipeline.NovoPedido(*id, *url, *inicio, *fim, time.Now())

	// Guarda contra misturar vídeo/transcrição de pedidos diferentes no mesmo id.
	dir := filepath.Join(*base, *id)
	var existente *pipeline.Pedido
	if p, err := pipeline.Carregar(*base, *id); err == nil {
		existente = p
	}
	switch decidirDownload(existente, ped, *force) {
	case recusarConflito:
		fmt.Fprintf(os.Stderr,
			"erro: o id %q já existe apontando para outro vídeo/janela.\n"+
				"  atual: %s [%s–%s]\n"+
				"  novo:  %s [%s–%s]\n"+
				"Use outro -id (recomendado, um por sermão) ou rode com -force para substituir "+
				"(apaga os artefatos atuais deste id).\n",
			*id, existente.YouTubeURL, existente.Inicio, existente.Fim, *url, *inicio, *fim)
		os.Exit(2)
	case limparERebaixar:
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintf(os.Stderr, "erro: não consegui limpar %s: %v\n", dir, err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "baixar: limpando id %q antes de rebaixar (evita misturar artefatos)\n", *id)
	}

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
