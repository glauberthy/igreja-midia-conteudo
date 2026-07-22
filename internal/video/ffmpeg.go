// Pacote video produz o Short final de cada candidato: corta o trecho do vídeo da
// pregação, reenquadra para 9:16 (vertical) e queima a legenda do trecho, gravando
// em finalizados/<id>/short_NN.mp4. O ffmpeg fica encapsulado atrás da interface
// Executor (mock nos testes).
//
// Alinhamento de tempo (importante): o video.mp4 vem recortado pela spec-03 e começa
// em t=0, enquanto os candidatos têm start/end em tempo ABSOLUTO do vídeo original
// (casados contra a transcrição). Por isso o corte é feito em (start - inicio) e a
// legenda do trecho é rebaseada a zero. Ver DP-005 (perfil visual) e DP-009 (9:16).
//
// Não altera o áudio/fala; a legenda vem da transcrição, sem reescrever palavras.
package video

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"srtclean/internal/pipeline"
	"srtclean/internal/transcricao"
	"srtclean/internal/validacao"
)

// Perfil visual (DP-005): vertical 1080x1920, legenda branca com contorno preto,
// centralizada embaixo. Enquadramento por crop central (DP-009: sair do quadro é ok).
const (
	larguraSaida  = 1080
	alturaSaida   = 1920
	estiloLegenda = "FontName=DejaVu Sans,Fontsize=16,PrimaryColour=&H00FFFFFF," +
		"OutlineColour=&H00000000,BorderStyle=1,Outline=2,Shadow=1,Alignment=2,MarginV=90"
)

// Executor roda um comando externo e devolve stdout, stderr e o erro de execução.
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

// Renderizador orquestra a geração dos Shorts. BaseDir é a raiz de trabalho
// (video.mp4/legenda.srt); OutDir é onde ficam os finais; Bin é o ffmpeg.
type Renderizador struct {
	Exec    Executor
	Bin     string
	BaseDir string
	OutDir  string
}

// NovoRenderizador cria um Renderizador com o executor real e os padrões.
func NovoRenderizador() *Renderizador {
	return &Renderizador{Exec: ExecutorReal{}, Bin: "ffmpeg", BaseDir: "trabalho", OutDir: "finalizados"}
}

func (r *Renderizador) bin() string {
	if r.Bin == "" {
		return "ffmpeg"
	}
	return r.Bin
}
func (r *Renderizador) baseDir() string {
	if r.BaseDir == "" {
		return "trabalho"
	}
	return r.BaseDir
}
func (r *Renderizador) outDir() string {
	if r.OutDir == "" {
		return "finalizados"
	}
	return r.OutDir
}

// Renderizar gera um Short por candidato, em ordem de score (maior primeiro), e
// devolve os caminhos gerados. Em falha, seta Status=erro e Erro. Os candidatos vêm
// SEMPRE de fora (spec-09: fonte única = arquivo de seleção validado); o pedido não
// os carrega mais.
func (r *Renderizador) Renderizar(ctx context.Context, ped *pipeline.Pedido, candidatos []validacao.Candidato) ([]string, error) {
	paths, err := r.renderizar(ctx, ped, candidatos)
	if err != nil {
		ped.Status = pipeline.EstadoErro
		ped.Erro = err.Error()
		return nil, err
	}
	return paths, nil
}

func (r *Renderizador) renderizar(ctx context.Context, ped *pipeline.Pedido, candidatos []validacao.Candidato) ([]string, error) {
	if len(candidatos) == 0 {
		return nil, fmt.Errorf("nenhum candidato para renderizar")
	}
	inicioMs, ok := transcricao.HmsToMs(ped.Inicio)
	if !ok {
		return nil, fmt.Errorf("início do pedido inválido: %q", ped.Inicio)
	}

	trabDir := filepath.Join(r.baseDir(), ped.ID)
	videoPath := filepath.Join(trabDir, "video.mp4")
	legendaPath := filepath.Join(trabDir, "legenda.srt")

	legendaRaw, err := os.ReadFile(legendaPath)
	if err != nil {
		return nil, fmt.Errorf("lendo legenda %q: %w", legendaPath, err)
	}
	cues := ParseSRT(string(legendaRaw))

	outDir := filepath.Join(r.outDir(), ped.ID)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, fmt.Errorf("criando pasta de saída: %w", err)
	}

	// Ordena por score (maior primeiro), mantendo a ordem original nos empates.
	ordenados := make([]validacao.Candidato, len(candidatos))
	copy(ordenados, candidatos)
	sort.SliceStable(ordenados, func(a, b int) bool {
		return ordenados[a].Score > ordenados[b].Score
	})

	var paths []string
	for i, cand := range ordenados {
		startMs, ok1 := transcricao.HmsToMs(cand.Start)
		endMs, ok2 := transcricao.HmsToMs(cand.End)
		if !ok1 || !ok2 || endMs <= startMs {
			return nil, fmt.Errorf("candidato %d com tempos inválidos: start=%q end=%q", i+1, cand.Start, cand.End)
		}

		// Corte relativo ao video.mp4 (que começa em t=0 no início da pregação).
		cutStartMs := startMs - inicioMs
		if cutStartMs < 0 {
			cutStartMs = 0
		}
		durMs := endMs - startMs

		// Legenda do trecho, recortada e rebaseada a zero.
		trechoCues := RecortarLegenda(cues, startMs, endMs)
		srtPath := filepath.Join(trabDir, fmt.Sprintf("short_%02d.srt", i+1))
		if err := os.WriteFile(srtPath, []byte(FormatarSRT(trechoCues)), 0644); err != nil {
			return nil, fmt.Errorf("gravando legenda do trecho: %w", err)
		}

		outPath := filepath.Join(outDir, fmt.Sprintf("short_%02d.mp4", i+1))
		args := ArgsFFmpeg(videoPath, srtPath, outPath, cutStartMs, durMs)

		if _, stderr, err := r.Exec.Rodar(ctx, r.bin(), args...); err != nil {
			return nil, fmt.Errorf("ffmpeg no short %02d: %w — %s", i+1, err, resumoStderr(stderr))
		}
		paths = append(paths, outPath)
	}

	return paths, nil
}

// ArgsFFmpeg monta os argumentos do ffmpeg para: cortar [cutStartMs, +durMs],
// reenquadrar 9:16 (crop central + scale) e queimar a legenda rebaseada.
func ArgsFFmpeg(videoPath, srtPath, outPath string, cutStartMs, durMs int) []string {
	filtro := fmt.Sprintf(
		"crop=ih*9/16:ih,scale=%d:%d,setsar=1,subtitles=%s:force_style='%s'",
		larguraSaida, alturaSaida, escaparFiltro(srtPath), estiloLegenda,
	)
	return []string{
		"-y",
		"-ss", segundos(cutStartMs),
		"-i", videoPath,
		"-t", segundos(durMs),
		"-vf", filtro,
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "20",
		"-c:a", "aac", "-b:a", "128k",
		"-movflags", "+faststart",
		outPath,
	}
}

// ParseSRT lê um .srt/.vtt em cues (início, fim, texto já limpo de marcação).
func ParseSRT(raw string) []Cue {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	raw = strings.TrimPrefix(raw, "\ufeff")

	var cues []Cue
	for _, b := range regexp.MustCompile(`\n[ \t]*\n`).Split(raw, -1) {
		b = strings.TrimSpace(b)
		if b == "" {
			continue
		}
		linhas := strings.Split(b, "\n")
		tIdx := -1
		for i, l := range linhas {
			if strings.Contains(l, "-->") {
				tIdx = i
				break
			}
		}
		if tIdx == -1 {
			continue
		}
		partes := strings.SplitN(linhas[tIdx], "-->", 2)
		if len(partes) != 2 {
			continue
		}
		ini, ok1 := transcricao.HmsToMs(strings.TrimSpace(partes[0]))
		fim, ok2 := transcricao.HmsToMs(strings.TrimSpace(campoTempo(partes[1])))
		if !ok1 || !ok2 {
			continue
		}
		texto := transcricao.CleanText(strings.Join(linhas[tIdx+1:], " "))
		if texto == "" {
			continue
		}
		cues = append(cues, Cue{InicioMs: ini, FimMs: fim, Texto: texto})
	}
	return cues
}

// RecortarLegenda (lógica pura) mantém as cues que se sobrepõem a [startMs, endMs],
// recorta às bordas e rebaseia os tempos a zero (relativos ao início do trecho).
func RecortarLegenda(cues []Cue, startMs, endMs int) []Cue {
	var out []Cue
	for _, c := range cues {
		if c.FimMs <= startMs || c.InicioMs >= endMs {
			continue // sem sobreposição
		}
		ini := maxInt(c.InicioMs, startMs) - startMs
		fim := minInt(c.FimMs, endMs) - startMs
		if fim <= ini {
			continue
		}
		out = append(out, Cue{InicioMs: ini, FimMs: fim, Texto: c.Texto})
	}
	return out
}

// FormatarSRT (lógica pura) serializa cues no formato .srt.
func FormatarSRT(cues []Cue) string {
	var sb strings.Builder
	for i, c := range cues {
		fmt.Fprintf(&sb, "%d\n%s --> %s\n%s\n\n", i+1, srtTime(c.InicioMs), srtTime(c.FimMs), c.Texto)
	}
	return sb.String()
}

// Cue é uma legenda com início, fim (ms) e o texto já limpo de marcação.
type Cue struct {
	InicioMs int
	FimMs    int
	Texto    string
}

// campoTempo pega só o horário de fim, ignorando ajustes de cue do VTT
// (ex.: "00:00:03.000 align:start position:0%").
func campoTempo(s string) string {
	return strings.Fields(strings.TrimSpace(s))[0]
}

// segundos formata ms como "S.mmm" para os flags -ss/-t do ffmpeg.
func segundos(ms int) string {
	return fmt.Sprintf("%d.%03d", ms/1000, ms%1000)
}

// srtTime formata ms como "HH:MM:SS,mmm".
func srtTime(ms int) string {
	h := ms / 3600000
	m := (ms % 3600000) / 60000
	s := (ms % 60000) / 1000
	milli := ms % 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, milli)
}

// escaparFiltro protege o caminho da legenda dentro do filtro do ffmpeg.
func escaparFiltro(p string) string {
	p = strings.ReplaceAll(p, `\`, `\\`)
	p = strings.ReplaceAll(p, ":", `\:`)
	p = strings.ReplaceAll(p, "'", `\'`)
	return "'" + p + "'"
}

func resumoStderr(b []byte) string {
	s := strings.TrimSpace(string(b))
	linhas := strings.Split(s, "\n")
	if n := len(linhas); n > 0 {
		return linhas[n-1]
	}
	return ""
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
