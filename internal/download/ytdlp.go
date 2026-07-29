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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"srtclean/internal/pipeline"
	"srtclean/internal/processo"
	"srtclean/internal/transcricao"
)

// Erros nomeados para o chamador distinguir os casos de falha.
var (
	ErrSemLegenda        = errors.New("vídeo sem legenda automática pt (DP-001: não transcrevemos localmente)")
	ErrVideoIndisponivel = errors.New("vídeo indisponível (privado, removido ou restrito)")
	ErrTempoInvalido     = errors.New("tempos inválidos: use HH:MM:SS com fim maior que início")
	// ErrAntiBot: o YouTube pediu verificação (HTTP 429 / "Sign in to confirm you're not
	// a bot"). É temporário e some sozinho depois de alguns minutos — merece uma mensagem
	// que NOMEIE o problema, senão vira um erro genérico e o operador não sabe o que fazer.
	ErrAntiBot = errors.New("o YouTube pediu verificação anti-robô (limite de requisições); aguarde alguns minutos e tente novamente")
)

// Retry do download (independente do retry do modelo, spec-08): o anti-bot do YouTube é
// TEMPORÁRIO, então esperar e refazer costuma resolver sozinho. Espera CRESCENTE para não
// insistir rápido demais — insistir prolonga o bloqueio.
const (
	maxTentativasDownload = 3
	esperaBaseDownload    = 30 * time.Second // 1ª espera; dobra a cada tentativa (30s, 60s)
)

// LogTentativaDownload registra cada tentativa que falhou (visível ao operador). Variável
// para os testes capturarem; em produção escreve no stderr.
var LogTentativaDownload = func(msg string) { fmt.Fprintln(os.Stderr, msg) }

// dormir é injetável nos testes (evita esperar de verdade).
var dormir = time.Sleep

// antiBot procura, no stderr do yt-dlp, as marcas de verificação anti-robô/limite.
func antiBot(stderr []byte) bool {
	s := strings.ToLower(string(stderr))
	for _, marca := range []string{
		"http error 429",
		"too many requests",
		"sign in to confirm you're not a bot",
		"sign in to confirm you’re not a bot", // apóstrofo tipográfico (o yt-dlp usa este)
		"confirm you are not a bot",
	} {
		if strings.Contains(s, marca) {
			return true
		}
	}
	return false
}

// comRetry roda `passo` até maxTentativasDownload, refazendo APENAS quando o erro é
// anti-bot/429 (temporário), com espera crescente entre as tentativas. Outros erros
// (vídeo indisponível, tempo inválido) falham na hora — refazer não ajudaria.
func comRetry(ctx context.Context, rotulo string, passo func() error) error {
	var err error
	for tentativa := 1; tentativa <= maxTentativasDownload; tentativa++ {
		err = passo()
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrAntiBot) {
			return err // erro definitivo: não insiste
		}
		if tentativa == maxTentativasDownload {
			break
		}
		espera := esperaBaseDownload * time.Duration(1<<(tentativa-1)) // 30s, 60s
		LogTentativaDownload(fmt.Sprintf(
			"%s: tentativa %d falhou (verificação anti-robô do YouTube); aguardando %s antes de refazer…",
			rotulo, tentativa, espera))
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		dormir(espera)
	}
	return fmt.Errorf("%s: %w (após %d tentativas)", rotulo, ErrAntiBot, maxTentativasDownload)
}

// Executor roda um comando externo e devolve stdout, stderr e o erro de execução.
// É a costura que permite injetar um mock nos testes.
type Executor interface {
	Rodar(ctx context.Context, nome string, args ...string) (stdout, stderr []byte, err error)
}

// ExecutorReal executa de fato o comando no sistema. Delega a internal/processo, que
// mata o GRUPO no cancelamento — sem isso o ffmpeg neto sobreviveria segurando o arquivo
// parcial, e o espaço em disco não voltaria mesmo depois da limpeza apagar o arquivo.
type ExecutorReal struct{}

func (ExecutorReal) Rodar(ctx context.Context, nome string, args ...string) ([]byte, []byte, error) {
	return processo.Rodar(ctx, nome, args...)
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

// FormatoPadrao é o seletor de formato do yt-dlp quando nenhum é informado.
//
// NÃO troque para <=720 "para economizar banda". As transmissões da igreja HOJE são 720p,
// então este seletor já baixa 720p — é o que existe. O teto em 1080 é intencional e
// preparatório: no dia em que a transmissão subir para 1080p, o pipeline aproveita
// sozinho, sem ninguém precisar lembrar de mudar este código. Isso importa porque o Short
// é um corte 9:16 do CENTRO: de uma fonte 720p sobram só 405x720 pixels reais (ampliados
// para 1080x1920), enquanto de 1080p sobrariam 608x1080 — 2,25x mais pixels reais. O teto
// existe para NÃO baixar 4K à toa (custo sem ganho para o nosso corte).
const FormatoPadrao = "bv*[height<=1080]+ba/b[height<=1080]"

func (b *Baixador) formato() string {
	if b.Formato == "" {
		return FormatoPadrao
	}
	return b.Formato
}

// Baixar executa o fluxo completo para o pedido. Em qualquer falha, preenche
// ped.Status = erro e ped.Erro com a mensagem, e devolve o erro nomeado.
//
// Ordem: fase leve (legenda + transcrição; ver BaixarLegenda) → baixa o vídeo do
// intervalo. É o caminho do cmd/baixar (CLI), que baixa tudo de uma vez.
//
// DEVOLVE A ORIGEM de tempo do video.mp4 escrito: o instante absoluto do vídeo original a
// que o t=0 do arquivo corresponde (aqui, o início da janela). Quem chama guarda onde quiser
// — tipicamente ped.DeclararOrigem. Só é válida quando err == nil.
//
// Por que devolver em vez de escrever no Pedido: escrever se perde em silêncio quando o
// chamador passa uma CÓPIA (é o caso do servidor, ver copiaPedido lá). Valor de retorno
// ignorado aparece no código de quem ignora; mutação descartada não aparece em lugar nenhum.
func (b *Baixador) Baixar(ctx context.Context, ped *pipeline.Pedido) (int, error) {
	origemMs, err := b.executar(ctx, ped)
	if err != nil {
		ped.Status = pipeline.EstadoErro
		ped.Erro = err.Error()
		return 0, err
	}
	return origemMs, nil
}

// BaixarLegenda baixa a legenda automática (sem o vídeo) para `dirDestino` — no servidor, a
// pasta do vídeo no CACHE (videos/<videoID>/), porque a legenda é do CULTO e serve qualquer
// janela e qualquer pedido. É a entrada do fluxo invertido do servidor web (spec-05):
// selecionar antes de baixar o vídeo pesado.
//
// NÃO gera transcrição. O recorte à janela é derivação, e derivar é do videocache
// (DerivarTranscricao) — quem baixa baixa, quem deriva deriva. Antes as duas coisas moravam
// aqui, e era isso que fazia a transcrição do cache nascer já recortada, inútil para outra
// janela.
//
// Se não houver legenda automática (DP-001), devolve ErrSemLegenda e nada de vídeo é baixado.
// Em qualquer falha, preenche ped.Status = erro e ped.Erro.
func (b *Baixador) BaixarLegenda(ctx context.Context, ped *pipeline.Pedido, dirDestino string) error {
	if err := b.baixarSRT(ctx, ped, dirDestino); err != nil {
		ped.Status = pipeline.EstadoErro
		ped.Erro = err.Error()
		return err
	}
	return nil
}

func (b *Baixador) executar(ctx context.Context, ped *pipeline.Pedido) (int, error) {
	if err := b.baixarLegenda(ctx, ped); err != nil {
		return 0, err
	}
	return b.baixarVideo(ctx, ped)
}

// baixarLegenda é o caminho do cmd/baixar (CLI): legenda E transcrição recortada à janela, as
// duas na pasta do PEDIDO. Mantido como era de propósito — é o caminho de diagnóstico fora do
// servidor, e ele funciona sozinho, sem cache. Não mexe em ped.Status (quem chama decide).
func (b *Baixador) baixarLegenda(ctx context.Context, ped *pipeline.Pedido) error {
	dir := filepath.Join(b.baseDir(), ped.ID)
	if err := b.baixarSRT(ctx, ped, dir); err != nil {
		return err
	}

	// Transcrição limpa (srtclean), recortada à janela da pregação [inicio, fim] MAS mantendo
	// os tempos ABSOLUTOS — para o corte do vídeo (spec-04) bater. A legenda automática vem
	// inteira; a transcrição da SELEÇÃO fica só com a pregação, para o modelo não escolher
	// trechos de louvor/avisos fora da janela.
	inicioMs, _ := transcricao.HmsToMs(ped.Inicio)
	fimMs, _ := transcricao.HmsToMs(ped.Fim)
	legenda := filepath.Join(dir, "legenda.srt")
	transc := filepath.Join(dir, "transcricao.txt")
	if _, _, err := transcricao.LimparArquivoJanela(legenda, transc, inicioMs, fimMs); err != nil {
		return fmt.Errorf("limpando legenda: %w", err)
	}
	return nil
}

// baixarSRT baixa a legenda automática pt (sem vídeo) para `dir`, normaliza o nome para
// legenda.srt e lê o título do .info.json. NÃO gera transcrição — quem quer transcrição
// deriva (CLI: baixarLegenda; servidor: videocache.DerivarTranscricao).
func (b *Baixador) baixarSRT(ctx context.Context, ped *pipeline.Pedido, dir string) error {
	if !tempoValido(ped.Inicio, ped.Fim) {
		return ErrTempoInvalido
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("criando pasta de destino: %w", err)
	}

	// Legenda automática pt (sem baixar o vídeo ainda). Com retry no anti-bot (temporário).
	err := comRetry(ctx, "baixando legenda", func() error {
		_, stderr, err := b.Exec.Rodar(ctx, b.bin(), argsLegenda(ped.YouTubeURL, dir, b.subLangs())...)
		if err != nil {
			if antiBot(stderr) {
				return ErrAntiBot
			}
			if indisponivel(stderr) {
				return ErrVideoIndisponivel
			}
			return fmt.Errorf("baixando legenda: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
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

	// Título do vídeo (best-effort): o --write-info-json grava um .info.json com o
	// "title". Não é essencial — se faltar, segue sem título (não trava o download).
	if t := lerTitulo(dir); t != "" {
		ped.Titulo = t
	}
	return nil
}

// baixarVideo baixa o trecho [inicio, fim] do vídeo e devolve a origem de tempo do arquivo.
// Pressupõe a pasta já criada pela fase leve. Não mexe em ped.Status (quem chama decide).
func (b *Baixador) baixarVideo(ctx context.Context, ped *pipeline.Pedido) (int, error) {
	return b.baixarVideoJanela(ctx, ped, ped.Inicio, ped.Fim)
}

// BaixarVideoCompleto baixa o vídeo INTEIRO com o downloader NATIVO do yt-dlp em paralelo
// (--concurrent-fragments), sem --download-sections. É a fase pesada do servidor (spec-05
// parte 3).
//
// Por que o vídeo inteiro, e não só as janelas aprovadas (medido, ver spec-05): o gargalo é
// PARALELISMO, não volume. Todo caminho que usa o ffmpeg como downloader (--download-sections,
// ou -ss numa URL) abre UMA conexão e o YouTube estrangula a ~84-174 KiB/s; o downloader
// nativo abre N fragmentos em paralelo e atinge ~26 MiB/s. Medição no mesmo vídeo: janela
// contígua de 18 min = 577 s; vídeo inteiro (46 min) = 7,3 s — ~79x mais rápido baixando
// MAIS bytes. De quebra, o contrato de tempo fica trivial: o arquivo começa no início do
// vídeo, então a origem é 0 e o corte de cada trecho usa o start/end ABSOLUTO.
//
// Em falha, preenche ped.Status = erro e ped.Erro.
//
// DEVOLVE A ORIGEM do arquivo escrito: aqui é sempre ZERO, porque o arquivo é o vídeo
// inteiro — o t=0 dele é o t=0 do vídeo do YouTube. Não é o `ped.Inicio`, que neste caminho
// é o início da PREGAÇÃO, coisa diferente. Devolver em vez de escrever no Pedido não é
// preciosismo: este método recebe uma CÓPIA do pedido no servidor, e uma atribuição feita
// aqui morreria com a cópia sem deixar rastro (foi assim que a origem virou bug). Ver o
// contrato completo em pipeline.Pedido.DeclararOrigem.
// `dirDestino` é onde o arquivo é escrito — no servidor, a pasta do vídeo no CACHE
// (videos/<videoID>/), porque o vídeo é do CULTO e serve qualquer janela e qualquer pedido.
// Quem chama decide o destino; este método só escreve e diz onde o arquivo começa.
func (b *Baixador) BaixarVideoCompleto(ctx context.Context, ped *pipeline.Pedido, dirDestino string) (int, error) {
	dir := dirDestino
	if err := os.MkdirAll(dir, 0755); err != nil {
		ped.Status = pipeline.EstadoErro
		ped.Erro = err.Error()
		return 0, fmt.Errorf("criando pasta de destino: %w", err)
	}
	err := comRetry(ctx, "baixando vídeo", func() error {
		_, stderr, err := b.Exec.Rodar(ctx, b.bin(), argsVideoCompleto(ped.YouTubeURL, dir, b.formato())...)
		if err != nil {
			if antiBot(stderr) {
				return ErrAntiBot
			}
			if indisponivel(stderr) {
				return ErrVideoIndisponivel
			}
			return fmt.Errorf("baixando vídeo: %w", err)
		}
		return nil
	})
	if err != nil {
		ped.Status = pipeline.EstadoErro
		ped.Erro = err.Error()
		return 0, err
	}
	return origemVideoInteiro, nil
}

// origemVideoInteiro é a origem de tempo de um arquivo que contém o vídeo INTEIRO: zero, por
// definição — o t=0 do arquivo é o t=0 do vídeo do YouTube.
//
// Nomeado em vez de um `0` solto porque zero aqui é uma AFIRMAÇÃO ("o arquivo começa no
// começo"), não um valor default. A diferença importa: `origem_ms` ausente significa "ninguém
// sabe" e faz o render recusar; `origem_ms: 0` significa "é o vídeo inteiro".
const origemVideoInteiro = 0

// BaixarVideoJanela baixa APENAS a janela [inicio, fim] do vídeo (via --download-sections).
// MANTIDO para o cmd/baixar (CLI) e compatibilidade; a fase pesada do servidor usa
// BaixarVideoCompleto, que é ~79x mais rápido (ver a nota lá). Em falha, preenche
// ped.Status = erro e ped.Erro.
// DEVOLVE A ORIGEM do arquivo escrito: o `inicio` da janela em ms (ver Baixar).
func (b *Baixador) BaixarVideoJanela(ctx context.Context, ped *pipeline.Pedido, inicio, fim string) (int, error) {
	dir := filepath.Join(b.baseDir(), ped.ID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		ped.Status = pipeline.EstadoErro
		ped.Erro = err.Error()
		return 0, fmt.Errorf("criando pasta de trabalho: %w", err)
	}
	origemMs, err := b.baixarVideoJanela(ctx, ped, inicio, fim)
	if err != nil {
		ped.Status = pipeline.EstadoErro
		ped.Erro = err.Error()
		return 0, err
	}
	return origemMs, nil
}

// baixarVideoJanela baixa a janela e DEVOLVE a origem de tempo do arquivo: o `inicio` em ms,
// porque o --download-sections rebaseia o arquivo a zero nesse instante.
func (b *Baixador) baixarVideoJanela(ctx context.Context, ped *pipeline.Pedido, inicio, fim string) (int, error) {
	dir := filepath.Join(b.baseDir(), ped.ID)
	origemMs, ok := transcricao.HmsToMs(inicio)
	if !ok {
		return 0, fmt.Errorf("%w: início %q não é HH:MM:SS", ErrTempoInvalido, inicio)
	}
	err := comRetry(ctx, "baixando vídeo", func() error {
		_, stderr, err := b.Exec.Rodar(ctx, b.bin(), argsVideo(ped.YouTubeURL, inicio, fim, dir, b.formato())...)
		if err != nil {
			if antiBot(stderr) {
				return ErrAntiBot
			}
			if indisponivel(stderr) {
				return ErrVideoIndisponivel
			}
			return fmt.Errorf("baixando vídeo: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return origemMs, nil
}

// argsLegenda monta o yt-dlp para baixar SÓ a legenda automática (idioma subLangs), em .srt.
// --force-overwrites garante que uma legenda pré-existente (de um download anterior) seja
// substituída, nunca reaproveitada — a legenda tem que corresponder à URL desta chamada.
func argsLegenda(url, dir, subLangs string) []string {
	return []string{
		"--no-playlist",
		"--force-overwrites",
		"--skip-download",
		"--write-info-json",
		"--write-auto-subs",
		"--sub-langs", subLangs,
		"--convert-subs", "srt",
		"-o", filepath.Join(dir, "legenda.%(ext)s"),
		url,
	}
}

// fragmentosParalelos é quantos fragmentos o downloader nativo puxa ao mesmo tempo. É o
// que destrava a velocidade: 1 conexão é estrangulada pelo YouTube (~174 KiB/s), 8 em
// paralelo atingem ~26 MiB/s (medido — ver spec-05).
const fragmentosParalelos = "8"

// argsVideoCompleto monta o yt-dlp para baixar o vídeo INTEIRO com o downloader NATIVO em
// paralelo. Sem --download-sections e sem --downloader-args de ffmpeg justamente porque
// esses caminhos entregam o download ao ffmpeg (conexão única, lenta); aqui queremos o
// downloader nativo com fragmentos concorrentes.
func argsVideoCompleto(url, dir, formato string) []string {
	return []string{
		"--no-playlist",
		// --force-overwrites: nunca reaproveitar silenciosamente um video.mp4 de outro pedido.
		"--force-overwrites",
		"--concurrent-fragments", fragmentosParalelos,
		"--merge-output-format", "mp4",
		"-f", formato,
		"-o", filepath.Join(dir, "video.%(ext)s"),
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
		// --force-overwrites é CRÍTICO: sem ele, o yt-dlp pula o download quando já existe
		// um video.mp4 na pasta ("has already been downloaded"), o que reaproveitaria
		// silenciosamente o vídeo de um pedido anterior. Com ele, o vídeo baixado sempre
		// corresponde à URL desta chamada.
		"--force-overwrites",
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

// lerTitulo lê o "title" do .info.json que o yt-dlp grava (--write-info-json). É
// best-effort: devolve "" se não houver arquivo ou o campo faltar.
func lerTitulo(dir string) string {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.info.json"))
	if len(matches) == 0 {
		return ""
	}
	b, err := os.ReadFile(matches[0])
	if err != nil {
		return ""
	}
	var info struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(b, &info); err != nil {
		return ""
	}
	return strings.TrimSpace(info.Title)
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
