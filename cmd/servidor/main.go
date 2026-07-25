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
	bin := flag.String("bin", "yt-dlp", "binário do yt-dlp")
	ffmpegBin := flag.String("ffmpeg", "ffmpeg", "binário do ffmpeg (fase pesada)")
	sublang := flag.String("sublang", "pt", "idioma da legenda automática (ex.: pt, pt-orig)")
	endpoint := flag.String("endpoint", harness.EndpointPadrao, "endpoint do modelo (llama-server; URL completa /v1/chat/completions)")
	prompts := flag.String("prompts", harness.PromptDirPadrao, "pasta dos prompts")
	declaracao := flag.String("declaracao", harness.DeclaracaoPadrao, "caminho da Declaração Doutrinária")
	flag.Parse()

	// O mesmo Baixador serve a fase leve (BaixarLegenda) e a pesada (BaixarVideoJanela).
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
		MargemFimMs: 0, RodapeAlpha: 1.00,
	}

	s := servidor.Novo(servidor.Opcoes{
		Baixador:       baixador,
		Selecionador:   sel,
		BaixadorVideo:  baixador,
		Renderizador:   rend,
		BaseDir:        *base,
		OutDir:         *out,
		LogRodadasPath: *logRodadas,
	})

	addr := fmt.Sprintf(":%d", *porta)
	log.Printf("servidor de Shorts no ar em http://localhost%s (Ctrl-C para sair)", addr)
	if err := http.ListenAndServe(addr, s); err != nil {
		fmt.Fprintf(os.Stderr, "erro ao subir o servidor: %v\n", err)
		os.Exit(1)
	}
}
