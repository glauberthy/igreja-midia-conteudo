// Comando servidor: sobe a interface web local do operador (spec-05). Uma única
// página (HTMX) numa porta local sem auth. O operador cola o link do culto e os
// tempos da pregação; o servidor baixa SÓ a legenda, roda a seleção e lista os
// trechos candidatos para revisão.
//
// Parte 1 da spec-05: servidor + fase leve (sem player, sem aprovar, sem render).
//
// Uso:
//
//	go run ./cmd/servidor            # sobe em :7799
//	go run ./cmd/servidor -porta 8090
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"srtclean/internal/download"
	"srtclean/internal/harness"
	"srtclean/internal/servidor"
	"srtclean/internal/validacao"
	"srtclean/internal/video"
	"srtclean/internal/videocache"
)

// selecionadorHarness adapta harness.Selecionar à interface Selecionador do servidor,
// fixando a Config (endpoint do modelo, prompts, declaração).
type selecionadorHarness struct {
	cfg harness.Config
}

func (s selecionadorHarness) Selecionar(ctx context.Context, transcricaoPath string) ([]validacao.Candidato, error) {
	return harness.Selecionar(ctx, transcricaoPath, s.cfg)
}

// analisadorFFmpeg adapta video.DetectarPausas à interface do servidor, fixando o executor e o
// binário — mesmo padrão do selecionadorHarness.
type analisadorFFmpeg struct {
	bin  string
	opts video.OpcoesPausas
}

func (a analisadorFFmpeg) Pausas(ctx context.Context, videoPath string) ([]video.Pausa, error) {
	return video.DetectarPausas(ctx, video.ExecutorReal{}, a.bin, videoPath, a.opts)
}

func main() {
	porta := flag.Int("porta", 7799, "porta TCP local do servidor (padrão 7799; evite 80/8080/8000)")
	base := flag.String("base", "trabalho", "pasta raiz de trabalho")
	out := flag.String("out", "finalizados", "pasta raiz dos Shorts finais")
	videos := flag.String("videos", videocache.DirPadrao, "raiz do cache por vídeo: videos/<idDoVídeo>/ "+
		"guarda vídeo+legenda do culto e serve qualquer janela e qualquer pedido (spec-05 v3)")
	logRodadas := flag.String("log", "resultados/rodadas.md", "arquivo de log das rodadas (avaliação de variância)")
	tempos := flag.String("tempos", "resultados/tempos.csv", "CSV de auditoria de desempenho (uma linha por pedido)")
	reter := flag.Int("reter", 1, "quantos pedidos mantêm o material bruto após a limpeza automática (spec-06)")
	videoDias := flag.Int("video-dias", videocache.DiasPadrao, "expiração do cache: apaga o vídeo do "+
		"culto sem uso há mais dias que isto (idade pelo ÚLTIMO USO)")
	videoTetoGB := flag.Int("video-teto", int(videocache.TetoPadrao>>30), "expiração do cache: teto em GB "+
		"— acima disso, apaga do uso mais antigo para o mais novo até caber")
	semLimpeza := flag.Bool("sem-limpeza", false, "desliga a limpeza automática de disco (use o cmd/limpar manualmente)")
	bin := flag.String("bin", "yt-dlp", "binário do yt-dlp")
	ffmpegBin := flag.String("ffmpeg", "ffmpeg", "binário do ffmpeg (fase pesada)")
	sublang := flag.String("sublang", "pt", "idioma da legenda automática (ex.: pt, pt-orig)")
	endpoint := flag.String("endpoint", harness.EndpointPadrao, "endpoint do modelo (llama-server; URL completa /v1/chat/completions)")
	prompts := flag.String("prompts", harness.PromptDirPadrao, "pasta dos prompts")
	declaracao := flag.String("declaracao", harness.DeclaracaoPadrao, "caminho da Declaração Doutrinária")
	legenda := flag.Bool("legenda", video.LegendaQueimadaPadrao, "queimar a legenda na imagem do Short "+
		"(spec-12 SUSPENSA: default desligado enquanto o timestamp for adiantado em ~3s)")
	pausaDB := flag.Int("pausa-db", video.PausaNoiseDBPadrao, "limiar de silêncio em dB para detectar "+
		"as PAUSAS de fala (fronteiras do corte; ver internal/video/pausas.go para a medição)")
	pausaMinMs := flag.Int("pausa-min-ms", video.PausaMinMsPadrao, "duração mínima (ms) para contar "+
		"como pausa: abaixo de ~200 ms o silencedetect pega micro-vão entre palavras e o corte "+
		"encaixaria no meio da frase")
	retomar := flag.String("retomar", "", "retoma um pedido já em disco, direto na revisão (pula legenda+seleção; "+
		"reaproveita o video.mp4 se existir). Para iterar em render/tela sem refazer o ciclo inteiro.")
	flag.Parse()

	// Contagem de retries para a auditoria de desempenho: os hooks de log do harness e do
	// download passam a incrementar o contador do pedido em curso, além de logar.
	logHarness, logDownload := harness.LogTentativa, download.LogTentativaDownload
	harness.LogTentativa = func(msg string) { servidor.ContarRetry(); logHarness(msg) }
	download.LogTentativaDownload = func(msg string) { servidor.ContarRetry(); logDownload(msg) }

	// O mesmo Baixador serve a fase leve (BaixarLegenda) e a pesada (BaixarVideoCompleto).
	baixador := &download.Baixador{
		Exec: download.ExecutorReal{}, Bin: *bin, BaseDir: *base, SubLangs: *sublang,
	}
	sel := selecionadorHarness{cfg: harness.Config{
		Endpoint:       *endpoint,
		PromptDir:      *prompts,
		DeclaracaoPath: *declaracao,
	}}
	// Render da fase pesada: margem-fim 0 (spec-10) e os padrões visuais (spec-12/13).
	rend := &video.Renderizador{
		Exec: video.ExecutorReal{}, Bin: *ffmpegBin, BaseDir: *base, OutDir: *out,
		MargemFimMs: 0,
		// Legenda queimada SUSPENSA (spec-12): explícita aqui pela mesma razão do RodapeAlpha
		// — o caminho do operador é ESTE, e um valor escrito à mão aqui já tornou a constante
		// do pacote letra morta uma vez. Vem da flag, cujo default é a constante do pacote.
		Legenda: *legenda,
		// RodapeAlpha PRECISA ser explícito: zero significa "sem gradiente" no contrato do
		// Renderizador, não "use o padrão". Referencia a constante exportada para o valor viver
		// num lugar só — antes daqui saía um 1.00 fixo, que anulava o padrão do pacote.
		// RodapeAltura fica zerado porque ali o zero SIM cai no padrão (rodapeAltura()).
		RodapeAlpha: video.RodapeAlphaPadrao,
	}

	s := servidor.Novo(servidor.Opcoes{
		Baixador:      baixador,
		Selecionador:  sel,
		BaixadorVideo: baixador,
		Renderizador:  rend,
		AnalisadorPausas: analisadorFFmpeg{bin: *ffmpegBin,
			opts: video.OpcoesPausas{NoiseDB: *pausaDB, MinMs: *pausaMinMs}},
		PausasDB:       *pausaDB,
		PausasMinMs:    *pausaMinMs,
		BaseDir:        *base,
		VideosDir:      *videos,
		OutDir:         *out,
		LogRodadasPath: *logRodadas,
		TemposPath:     *tempos,
		ReterPedidos:   *reter,
		// Mínimo 1 nos dois: zero, no contrato do pacote, quer dizer "use o padrão" — então um
		// `-video-teto 0` digitado para esvaziar o cache viraria 50 GB, o oposto do pedido.
		VideoDias:        max(*videoDias, 1),
		VideoTeto:        int64(max(*videoTetoGB, 1)) << 30,
		LimpezaDesligada: *semLimpeza,
	})

	// Retomada: falha na SUBIDA se o pedido não servir. Melhor um erro claro aqui que o
	// operador abrir a tela e encontrar o formulário vazio, sem entender por que.
	if *retomar != "" {
		if err := s.Retomar(*retomar); err != nil {
			fmt.Fprintf(os.Stderr, "erro ao retomar: %v\n", err)
			os.Exit(1)
		}
		log.Printf("pedido %s retomado: abra a página e ele estará na revisão", *retomar)
	}

	addr := fmt.Sprintf(":%d", *porta)
	srv := &http.Server{Addr: addr, Handler: s}

	// ENCERRAMENTO: registra no tempos.csv os pedidos que estavam em curso.
	//
	// Sem isto, abandono é invisível: só quem termina escreve no CSV, então falha aparece e
	// interrupção não — e a medição fica com viés otimista (só os ciclos que chegaram ao fim).
	// É o mesmo viés que o cortes.csv tinha. E abandono é o dado mais informativo que faltava:
	// ciclo interrompido é sintoma de algo ruim demais para terminar.
	//
	// Ctrl-C e SIGTERM (o que o systemd/docker mandam). SIGKILL não dá para tratar — e essa
	// lacuna é honesta: `kill -9` continua sumindo do CSV.
	sinais := make(chan os.Signal, 1)
	signal.Notify(sinais, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sinais
		if n := s.RegistrarAbandonados(""); n > 0 {
			log.Printf("encerrando (%s): %d pedido(s) em curso registrado(s) no CSV como não concluídos", sig, n)
		}
		ctx, cancelar := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancelar()
		srv.Shutdown(ctx)
	}()

	log.Printf("servidor de Shorts no ar em http://localhost%s (Ctrl-C para sair)", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "erro ao subir o servidor: %v\n", err)
		os.Exit(1)
	}
}
