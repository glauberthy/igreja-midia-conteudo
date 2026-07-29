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

	"srtclean/internal/download"
	"srtclean/internal/harness"
	"srtclean/internal/servidor"
	"srtclean/internal/validacao"
	"srtclean/internal/video"
)

// selecionadorHarness adapta harness.Selecionar à interface Selecionador do servidor,
// fixando a Config (endpoint do modelo, prompts, declaração).
type selecionadorHarness struct {
	cfg harness.Config
}

func (s selecionadorHarness) Selecionar(ctx context.Context, transcricaoPath string) ([]validacao.Candidato, error) {
	return harness.Selecionar(ctx, transcricaoPath, s.cfg)
}

func main() {
	porta := flag.Int("porta", 7799, "porta TCP local do servidor (padrão 7799; evite 80/8080/8000)")
	base := flag.String("base", "trabalho", "pasta raiz de trabalho")
	out := flag.String("out", "finalizados", "pasta raiz dos Shorts finais")
	logRodadas := flag.String("log", "resultados/rodadas.md", "arquivo de log das rodadas (avaliação de variância)")
	tempos := flag.String("tempos", "resultados/tempos.csv", "CSV de auditoria de desempenho (uma linha por pedido)")
	reter := flag.Int("reter", 1, "quantos pedidos mantêm o material bruto após a limpeza automática (spec-06)")
	semLimpeza := flag.Bool("sem-limpeza", false, "desliga a limpeza automática de disco (use o cmd/limpar manualmente)")
	bin := flag.String("bin", "yt-dlp", "binário do yt-dlp")
	ffmpegBin := flag.String("ffmpeg", "ffmpeg", "binário do ffmpeg (fase pesada)")
	sublang := flag.String("sublang", "pt", "idioma da legenda automática (ex.: pt, pt-orig)")
	endpoint := flag.String("endpoint", harness.EndpointPadrao, "endpoint do modelo (llama-server; URL completa /v1/chat/completions)")
	prompts := flag.String("prompts", harness.PromptDirPadrao, "pasta dos prompts")
	declaracao := flag.String("declaracao", harness.DeclaracaoPadrao, "caminho da Declaração Doutrinária")
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
		// RodapeAlpha/RodapeAltura zerados = usa o padrão medido do pacote video (0.80/1400).
		// Antes o servidor fixava 1.00 aqui, então mudar o padrão do pacote não teria efeito
		// nenhum no caminho que o operador usa — a constante existia e era letra morta.
	}

	s := servidor.Novo(servidor.Opcoes{
		Baixador:         baixador,
		Selecionador:     sel,
		BaixadorVideo:    baixador,
		Renderizador:     rend,
		BaseDir:          *base,
		OutDir:           *out,
		LogRodadasPath:   *logRodadas,
		TemposPath:       *tempos,
		ReterPedidos:     *reter,
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
	log.Printf("servidor de Shorts no ar em http://localhost%s (Ctrl-C para sair)", addr)
	if err := http.ListenAndServe(addr, s); err != nil {
		fmt.Fprintf(os.Stderr, "erro ao subir o servidor: %v\n", err)
		os.Exit(1)
	}
}
