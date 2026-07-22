// Pacote download obtém o material bruto de um pedido (legenda automática pt e o
// vídeo do trecho da pregação) encapsulando o yt-dlp como subprocesso. Depois passa
// a legenda pelo srtclean (internal/transcricao) e grava a transcrição.
//
// DP-001 (BRD): sem transcrição local. Se não houver legenda automática pt, o
// processo PARA com erro claro (ErrSemLegenda) — nunca tenta transcrever o áudio.
//
// O yt-dlp é dependência externa de sistema (ver README). O Go só o invoca, atrás
// da interface Executor, o que permite testar o fluxo sem acessar a internet.
package download

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"srtclean/internal/pipeline"
	"srtclean/internal/transcricao"
)

// Erros nomeados para o chamador distinguir os casos de falha.
var (
	ErrSemLegenda        = errors.New("vídeo sem legenda automática pt (DP-001: não transcrevemos localmente)")
	ErrVideoIndisponivel = errors.New("vídeo indisponível (privado, removido ou restrito)")
	ErrTempoInvalido     = errors.New("tempos inválidos: use HH:MM:SS com fim maior que início")
)

// Executor roda um comando externo e devolve stdout, stderr e o erro de execução.
// É a costura que permite injetar um mock nos testes.
type Executor interface {
	Rodar(ctx context.Context, nome string, args ...string) (stdout, stderr []byte, err error)
}

// ExecutorReal executa de fato o comando no sistema.
type ExecutorReal struct{}

func (ExecutorReal) Rodar(ctx context.Context, nome string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, nome, args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	return out.Bytes(), errb.Bytes(), err
}

// Baixador orquestra o download de um pedido. BaseDir é a raiz das pastas de
// trabalho (padrão "trabalho"); Bin é o binário do yt-dlp (padrão "yt-dlp").
type Baixador struct {
	Exec     Executor
	Bin      string
	BaseDir  string
	SubLangs string // idioma(s) da legenda automática (padrão "pt")
	Formato  string // seletor de formato do yt-dlp (-f); vazio = melhor
}

// NovoBaixador cria um Baixador com o executor real e os padrões.
func NovoBaixador() *Baixador {
	return &Baixador{Exec: ExecutorReal{}, Bin: "yt-dlp", BaseDir: "trabalho", SubLangs: "pt"}
}

func (b *Baixador) subLangs() string {
	if b.SubLangs == "" {
		return "pt"
	}
	return b.SubLangs
}

func (b *Baixador) bin() string {
	if b.Bin == "" {
		return "yt-dlp"
	}
	return b.Bin
}

func (b *Baixador) baseDir() string {
	if b.BaseDir == "" {
		return "trabalho"
	}
	return b.BaseDir
}

// Baixar executa o fluxo completo para o pedido. Em qualquer falha, preenche
// ped.Status = erro e ped.Erro com a mensagem, e devolve o erro nomeado.
//
// Ordem: valida tempos → baixa a legenda (barato, --skip-download) → se não houver
// legenda, PARA antes de baixar o vídeo → baixa o vídeo do intervalo → gera a
// transcrição via srtclean.
func (b *Baixador) Baixar(ctx context.Context, ped *pipeline.Pedido) error {
	if err := b.executar(ctx, ped); err != nil {
		ped.Status = pipeline.EstadoErro
		ped.Erro = err.Error()
		return err
	}
	return nil
}

func (b *Baixador) executar(ctx context.Context, ped *pipeline.Pedido) error {
	if !tempoValido(ped.Inicio, ped.Fim) {
		return ErrTempoInvalido
	}

	dir := filepath.Join(b.baseDir(), ped.ID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("criando pasta de trabalho: %w", err)
	}

	// 1) Legenda automática pt (sem baixar o vídeo ainda).
	_, stderr, err := b.Exec.Rodar(ctx, b.bin(), argsLegenda(ped.YouTubeURL, dir, b.subLangs())...)
	if err != nil {
		if indisponivel(stderr) {
			return ErrVideoIndisponivel
		}
		return fmt.Errorf("baixando legenda: %w", err)
	}

	srt, ok := acharSRT(dir)
	if !ok {
		return ErrSemLegenda
	}
	legenda := filepath.Join(dir, "legenda.srt")
	if srt != legenda {
		if err := os.Rename(srt, legenda); err != nil {
			return fmt.Errorf("renomeando legenda: %w", err)
		}
	}

	// 2) Vídeo do intervalo da pregação.
	_, stderr, err = b.Exec.Rodar(ctx, b.bin(), argsVideo(ped.YouTubeURL, ped.Inicio, ped.Fim, dir, b.Formato)...)
	if err != nil {
		if indisponivel(stderr) {
			return ErrVideoIndisponivel
		}
		return fmt.Errorf("baixando vídeo: %w", err)
	}

	// 3) Transcrição limpa (srtclean), recortada à janela da pregação [inicio, fim]
	//    MAS mantendo os tempos ABSOLUTOS — para o corte do vídeo (spec-04) bater
	//    (video.mp4 rebaseado a zero; corte em start-inicio). A legenda automática
	//    é baixada inteira; aqui a transcrição da SELEÇÃO fica só com a pregação, para
	//    o modelo não escolher trechos de louvor/avisos fora da janela.
	inicioMs, _ := transcricao.HmsToMs(ped.Inicio)
	fimMs, _ := transcricao.HmsToMs(ped.Fim)
	transc := filepath.Join(dir, "transcricao.txt")
	if _, _, err := transcricao.LimparArquivoJanela(legenda, transc, inicioMs, fimMs); err != nil {
		return fmt.Errorf("limpando legenda: %w", err)
	}

	return nil
}

// argsLegenda monta o yt-dlp para baixar SÓ a legenda automática (idioma subLangs), em .srt.
func argsLegenda(url, dir, subLangs string) []string {
	return []string{
		"--no-playlist",
		"--skip-download",
		"--write-auto-subs",
		"--sub-langs", subLangs,
		"--convert-subs", "srt",
		"-o", filepath.Join(dir, "legenda.%(ext)s"),
		url,
	}
}

// argsVideo monta o yt-dlp para baixar apenas o trecho [inicio, fim] do vídeo.
// As flags de reconexão do ffmpeg (--downloader-args ffmpeg_i:...) evitam que uma
// conexão travada do googlevideo pendure o download indefinidamente. formato vazio
// deixa o yt-dlp escolher o melhor.
func argsVideo(url, inicio, fim, dir, formato string) []string {
	args := []string{
		"--no-playlist",
		"--download-sections", "*" + inicio + "-" + fim,
		"--force-keyframes-at-cuts",
		"--downloader-args", "ffmpeg_i:-reconnect 1 -reconnect_streamed 1 -reconnect_delay_max 30 -rw_timeout 30000000",
		"--merge-output-format", "mp4",
	}
	if formato != "" {
		args = append(args, "-f", formato)
	}
	args = append(args,
		"-o", filepath.Join(dir, "video.%(ext)s"),
		url,
	)
	return args
}

// acharSRT devolve o primeiro .srt encontrado em dir (yt-dlp nomeia como legenda.pt.srt).
func acharSRT(dir string) (string, bool) {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.srt"))
	if len(matches) == 0 {
		return "", false
	}
	return matches[0], true
}

// tempoValido confere formato HH:MM:SS em ambos e que fim > início.
func tempoValido(inicio, fim string) bool {
	i, oki := transcricao.HmsToMs(inicio)
	f, okf := transcricao.HmsToMs(fim)
	if !oki || !okf || inicio == "" || fim == "" {
		return false
	}
	return f > i
}

// indisponivel procura, no stderr do yt-dlp, marcas de vídeo indisponível.
func indisponivel(stderr []byte) bool {
	s := strings.ToLower(string(stderr))
	for _, marca := range []string{
		"video unavailable",
		"private video",
		"is not available",
		"removed",
		"account associated with this video has been terminated",
	} {
		if strings.Contains(s, marca) {
			return true
		}
	}
	return false
}
