// Pacote video produz o Short final de cada candidato: corta o trecho do vídeo da
// pregação, reenquadra para 9:16 (vertical) e queima a legenda do trecho, gravando
// em finalizados/<id>/short_NN.mp4. O ffmpeg fica encapsulado atrás da interface
// Executor (mock nos testes).
//
// Alinhamento de tempo (importante): o video.mp4 vem recortado pela spec-03 e começa
// em t=0, enquanto os candidatos têm start/end em tempo ABSOLUTO do vídeo original
// (casados contra a transcrição). Por isso o corte é feito em (start - inicio) e a
// legenda é rebaseada a zero. Ver DP-005 (perfil visual) e DP-009 (9:16).
//
// Legenda (spec-12): NÃO se queima o SRT bruto rolling do YouTube. O texto vem do TEXTO
// LIMPO da Fase 3 (harness.Frasear, via internal/video/legenda.go), em blocos de 1–2
// linhas, na BASE do vídeo (acima da faixa reservada à logo), com fonte Google Sans Flex
// encorpada carregada direto do .ttf (drawtext:fontfile), branca com contorno/sombra.
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
	"sort"
	"strings"

	"srtclean/internal/harness"
	"srtclean/internal/pipeline"
	"srtclean/internal/transcricao"
	"srtclean/internal/validacao"
)

// Perfil visual (DP-005 / DP-009): vertical 1080x1920, crop central.
const (
	larguraSaida = 1080
	alturaSaida  = 1920
)

// Legenda queimada (spec-12). São os parâmetros de CALIBRAÇÃO visual — fáceis de ajustar
// vendo o resultado no vídeo. Fonte/tamanho/ritmo também são configuráveis por flag
// (ver cmd/render e os campos do Renderizador); estes são os defaults.
const (
	// Fonte Google Sans Flex encorpada, carregada direto do .ttf (não depende de instalar
	// no sistema). Peso trocável apontando para outro arquivo (ex.: ...ExtraBold.ttf).
	fonteLegendaPadrao = "assets/fontes/static/GoogleSansFlex_72pt-Bold.ttf"
	tamanhoFontePadrao = 54 // px em 1080x1920 (calibrável)
	// Largura da linha em caracteres: governa o RITMO de troca dos blocos (calibrável).
	charsPorLinhaPadrao = 32
	maxLinhasLegenda    = 2   // spec-12: no máximo 2 linhas por vez, nunca 4
	contornoLegenda     = 4   // borderw preto (legível sobre qualquer fundo)
	sombraLegenda       = 2   // shadowx/shadowy
	espacoLinhasLegenda = 10  // line_spacing
	faixaLogoPx         = 240 // faixa inferior RESERVADA à logo; a legenda fica acima dela
)

// Logo no rodapé (spec-13). Sobreposta (overlay do PNG com alpha) na faixa reservada,
// centralizada. Também uma faixa escura semitransparente no rodapé: garante que o texto
// BRANCO da logo seja legível mesmo sobre fundo claro (o rodapé deste vídeo é claro) e,
// de quebra, reforça a legenda. Tudo calibrável (flag/constante).
const (
	logoPathPadrao        = "assets/ibi_assinatura_shorts.png"
	logoLarguraPadrao     = 560 // largura da logo no vídeo (px), aspecto preservado
	logoMargemBaixoPadrao = 64  // px do fundo até a base da logo
	// Faixa escura do rodapé como GRADIENTE (transparente em cima → escuro embaixo),
	// suave como na arte de referência (não uma caixa de borda dura). A opacidade sobe com
	// uma curva (pow) para o começo ser IMPERCEPTÍVEL — sem linha visível no topo.
	rodapeAlphaPadrao  = 0.92 // opacidade máxima do gradiente (na base); 0 = sem gradiente
	rodapeAlturaPadrao = 900  // altura do gradiente (px), de baixo para cima
	easeGradiente      = 2.2  // expoente da curva de opacidade (>1 = começa mais suave)
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
// (video.mp4 + transcricao.txt); OutDir é onde ficam os finais; Bin é o ffmpeg.
//
// MargemFimMs é a margem de recuo no fim do corte (spec-10): cada Short termina em
// (end - margem) em vez de exatamente no `end`, para não capturar o começo da fala
// seguinte (a legenda automática do YouTube atrasa em relação ao áudio). O `end`
// calculado pela Fase 3 NÃO muda — o recuo é só no corte. 0 = sem margem.
type Renderizador struct {
	Exec        Executor
	Bin         string
	BaseDir     string
	OutDir      string
	MargemFimMs int

	// Calibração da legenda (spec-12); zero/"" usa os defaults acima.
	FontePath     string // caminho do .ttf da fonte
	TamanhoFonte  int    // px
	CharsPorLinha int    // largura da linha (ritmo de troca dos blocos)

	// Logo e rodapé (spec-13); zero/"" usa os defaults acima.
	LogoPath        string  // PNG da logo; se o arquivo não existir, renderiza sem logo
	LogoLargura     int     // largura da logo no vídeo (px)
	LogoMargemBaixo int     // px do fundo até a base da logo
	RodapeAlpha     float64 // opacidade do gradiente escuro na base (0 = sem gradiente)
	RodapeAltura    int     // altura do gradiente escuro (px)
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
func (r *Renderizador) fontePath() string {
	if r.FontePath == "" {
		return fonteLegendaPadrao
	}
	return r.FontePath
}
func (r *Renderizador) tamanhoFonte() int {
	if r.TamanhoFonte <= 0 {
		return tamanhoFontePadrao
	}
	return r.TamanhoFonte
}
func (r *Renderizador) charsPorLinha() int {
	if r.CharsPorLinha <= 0 {
		return charsPorLinhaPadrao
	}
	return r.CharsPorLinha
}
func (r *Renderizador) logoPath() string {
	if r.LogoPath == "" {
		return logoPathPadrao
	}
	return r.LogoPath
}
func (r *Renderizador) logoLargura() int {
	if r.LogoLargura <= 0 {
		return logoLarguraPadrao
	}
	return r.LogoLargura
}
func (r *Renderizador) logoMargemBaixo() int {
	if r.LogoMargemBaixo <= 0 {
		return logoMargemBaixoPadrao
	}
	return r.LogoMargemBaixo
}
func (r *Renderizador) rodapeAltura() int {
	if r.RodapeAltura <= 0 {
		return rodapeAlturaPadrao
	}
	return r.RodapeAltura
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

	// Texto LIMPO da legenda: vem da transcrição já limpa (mesma que a seleção usa),
	// passada pela desduplicação/segmentação da Fase 3 (harness.Frasear). NÃO usamos o
	// SRT bruto rolling (spec-12).
	transcPath := filepath.Join(trabDir, "transcricao.txt")
	transcBytes, err := os.ReadFile(transcPath)
	if err != nil {
		return nil, fmt.Errorf("lendo transcrição %q (necessária p/ a legenda limpa): %w", transcPath, err)
	}
	frases := harness.Frasear(string(transcBytes))

	est := EstiloLegenda{
		FontePath:    r.fontePath(),
		Tamanho:      r.tamanhoFonte(),
		Contorno:     contornoLegenda,
		Sombra:       sombraLegenda,
		EspacoLinhas: espacoLinhasLegenda,
		FaixaLogoPx:  faixaLogoPx,
	}
	cpl := r.charsPorLinha()
	grad := GradConfig{Altura: r.rodapeAltura(), Alpha: r.RodapeAlpha}

	// Logo do rodapé (spec-13): só sobrepõe se o PNG existir (senão renderiza sem logo,
	// com um aviso — não trava o Short por causa da marca).
	logoPath := r.logoPath()
	comLogo := false
	if _, err := os.Stat(logoPath); err == nil {
		comLogo = true
	} else {
		fmt.Fprintf(os.Stderr, "aviso: logo não encontrada em %s; renderizando sem logo\n", logoPath)
	}
	logo := LogoConfig{Path: logoPath, LarguraPx: r.logoLargura(), MargemBaixo: r.logoMargemBaixo()}

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
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("candidato %d com tempos inválidos: start=%q end=%q", i+1, cand.Start, cand.End)
		}

		// Corte relativo ao video.mp4 (que começa em t=0 no início da pregação).
		cutStartMs := startMs - inicioMs
		if cutStartMs < 0 {
			cutStartMs = 0
		}
		// Duração do corte com o recuo de margem no fim (spec-10). O `end` original é
		// preservado na legenda; só o corte de vídeo termina em (end - margem).
		durMs, err := duracaoComMargem(startMs, endMs, r.MargemFimMs)
		if err != nil {
			return nil, fmt.Errorf("candidato %d: %w", i+1, err)
		}

		// Legenda: blocos de texto LIMPO (Frasear) dentro do trecho, rebaseados a zero.
		// Cada bloco vira um arquivo de texto (evita escaping) referenciado pelo drawtext.
		blocos := BlocosLegenda(frases, startMs, endMs, cpl, maxLinhasLegenda)
		var usados []BlocoLegenda
		var textfiles []string
		for k, bl := range blocos {
			if bl.InicioMs >= durMs { // bloco além do corte (margem pode ter encurtado)
				continue
			}
			if bl.FimMs > durMs {
				bl.FimMs = durMs
			}
			if bl.FimMs <= bl.InicioMs {
				continue
			}
			tf := filepath.Join(trabDir, fmt.Sprintf("short_%02d.sub%03d.txt", i+1, k+1))
			if err := os.WriteFile(tf, []byte(bl.Texto), 0644); err != nil {
				return nil, fmt.Errorf("gravando bloco de legenda: %w", err)
			}
			usados = append(usados, bl)
			textfiles = append(textfiles, tf)
		}
		filtro, complexo := montarFiltro(usados, textfiles, est, comLogo, logo, grad)

		outPath := filepath.Join(outDir, fmt.Sprintf("short_%02d.mp4", i+1))
		logoInput := ""
		if comLogo {
			logoInput = logo.Path
		}
		args := ArgsFFmpeg(videoPath, logoInput, outPath, cutStartMs, durMs, filtro, complexo)

		if _, stderr, err := r.Exec.Rodar(ctx, r.bin(), args...); err != nil {
			return nil, fmt.Errorf("ffmpeg no short %02d: %w — %s", i+1, err, resumoStderr(stderr))
		}
		paths = append(paths, outPath)
	}

	return paths, nil
}

// duracaoComMargem devolve a duração do corte (ms) recuando `margemMs` do fim do trecho
// (spec-10), para o Short não capturar o começo da fala seguinte. O `end` original (fim
// de frase, marcado pela Fase 3) é preservado — só o corte apara a margem. Guarda contra
// margem que zere ou inverta o trecho: se (end - margem) <= start, é erro claro, não corte.
func duracaoComMargem(startMs, endMs, margemMs int) (int, error) {
	dur := endMs - startMs
	if dur <= 0 {
		return 0, fmt.Errorf("trecho vazio ou invertido: start=%dms end=%dms", startMs, endMs)
	}
	if margemMs <= 0 {
		return dur, nil // sem margem: corte termina no end original
	}
	ajustado := dur - margemMs
	if ajustado <= 0 {
		return 0, fmt.Errorf("margem de fim (%dms) >= duração do trecho (%dms): o recuo inverteria o trecho", margemMs, dur)
	}
	return ajustado, nil
}

// ArgsFFmpeg monta os argumentos do ffmpeg: cortar [cutStartMs, +durMs] e aplicar o
// filtro. Se `complexo`, usa -filter_complex (o filtro deve terminar em [vout]) e mapeia
// vídeo+áudio; senão, usa -vf. logoPath != "" entra como 2º input (referenciável por [1:v]).
func ArgsFFmpeg(videoPath, logoPath, outPath string, cutStartMs, durMs int, filtro string, complexo bool) []string {
	args := []string{"-y", "-ss", segundos(cutStartMs), "-i", videoPath}
	if logoPath != "" {
		args = append(args, "-i", logoPath)
	}
	if complexo {
		args = append(args, "-filter_complex", filtro, "-map", "[vout]", "-map", "0:a?")
	} else {
		args = append(args, "-vf", filtro)
	}
	args = append(args,
		"-t", segundos(durMs),
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "20",
		"-c:a", "aac", "-b:a", "128k",
		"-movflags", "+faststart",
		outPath,
	)
	return args
}

// EstiloLegenda são os parâmetros visuais da legenda queimada (spec-12).
type EstiloLegenda struct {
	FontePath    string // .ttf carregado direto (drawtext:fontfile)
	Tamanho      int    // fontsize (px)
	Contorno     int    // borderw (contorno preto)
	Sombra       int    // shadowx/shadowy
	EspacoLinhas int    // line_spacing
	FaixaLogoPx  int    // faixa inferior reservada à logo (legenda fica acima)
}

// LogoConfig são os parâmetros da logo sobreposta no rodapé (spec-13).
type LogoConfig struct {
	Path        string // PNG com alpha
	LarguraPx   int    // largura no vídeo (aspecto preservado)
	MargemBaixo int    // px do fundo até a base da logo
}

// GradConfig é o gradiente escuro do rodapé (spec-13): transparente em cima → escuro
// embaixo, para dar legibilidade à logo/legenda sobre fundo claro, de forma suave.
type GradConfig struct {
	Altura int     // altura do gradiente (px), medido do fundo para cima
	Alpha  float64 // opacidade máxima (na base); 0 = sem gradiente
}

func (g GradConfig) ativo() bool { return g.Alpha > 0 && g.Altura > 0 }

// filtroBase é o reenquadramento comum: crop central 9:16 + scale para 1080x1920.
func filtroBase() string {
	return fmt.Sprintf("crop=ih*9/16:ih,scale=%d:%d,setsar=1", larguraSaida, alturaSaida)
}

// drawtextFiltros (lógica pura) monta a cadeia de drawtext (um por bloco de legenda),
// juntada por vírgula, sem vírgula inicial (vazio se não há blocos). Cada drawtext carrega
// a fonte direto do .ttf, branca com contorno/sombra, centralizada, ancorada na BASE acima
// da faixa da logo, visível só na janela do bloco. Texto vem de arquivo (sem escaping).
func drawtextFiltros(blocos []BlocoLegenda, textfiles []string, est EstiloLegenda) string {
	var fs []string
	for i, bl := range blocos {
		fs = append(fs, fmt.Sprintf(
			"drawtext=fontfile=%s:textfile=%s:expansion=none"+
				":fontsize=%d:fontcolor=white:borderw=%d:bordercolor=black"+
				":shadowcolor=black@0.55:shadowx=%d:shadowy=%d:line_spacing=%d:text_align=C"+
				":x=(w-text_w)/2:y=h-%d-text_h:enable='between(t,%s,%s)'",
			escaparFiltro(est.FontePath), escaparFiltro(textfiles[i]),
			est.Tamanho, est.Contorno, est.Sombra, est.Sombra, est.EspacoLinhas,
			est.FaixaLogoPx, segundos(bl.InicioMs), segundos(bl.FimMs),
		))
	}
	return strings.Join(fs, ",")
}

// montarFiltro (lógica pura) decide entre -vf (simples) e -filter_complex e monta o
// filtro completo: reenquadramento → gradiente escuro do rodapé (se ativo) → legenda →
// logo (se comLogo). Devolve o filtro e se é filter_complex (saída rotulada [vout]).
// Ordem de empilhamento (spec-13): vídeo, gradiente, legenda, logo — a legenda fica sobre
// o gradiente e a logo por cima de tudo, no rodapé.
func montarFiltro(blocos []BlocoLegenda, textfiles []string, est EstiloLegenda, comLogo bool, logo LogoConfig, grad GradConfig) (string, bool) {
	base := filtroBase()
	dts := drawtextFiltros(blocos, textfiles, est)

	// Caso simples: sem logo e sem gradiente → cadeia -vf única.
	if !comLogo && !grad.ativo() {
		if dts == "" {
			return base, false
		}
		return base + "," + dts, false
	}

	var segs []string
	label := "v0"
	segs = append(segs, "[0:v]"+base+"["+label+"]")
	if grad.ativo() {
		// Gradiente preto com alpha crescente (curva pow) — começo imperceptível, sem
		// borda dura. `color` gera 1 frame preto; `geq` põe o alpha em rampa suave.
		segs = append(segs, fmt.Sprintf(
			"color=c=black:s=%dx%d:d=1,format=rgba,geq=r=0:g=0:b=0:a='%.2f*255*pow(Y/H\\,%.1f)'[grad]",
			larguraSaida, grad.Altura, grad.Alpha, easeGradiente))
		segs = append(segs, fmt.Sprintf("[%s][grad]overlay=0:H-h[vg]", label))
		label = "vg"
	}
	if dts != "" {
		segs = append(segs, fmt.Sprintf("[%s]%s[vt]", label, dts))
		label = "vt"
	}
	if comLogo {
		segs = append(segs, fmt.Sprintf("[1:v]scale=%d:-2[logo]", logo.LarguraPx))
		segs = append(segs, fmt.Sprintf("[%s][logo]overlay=x=(W-w)/2:y=H-%d-h[vout]", label, logo.MargemBaixo))
		label = "vout"
	}
	if label != "vout" { // garante o rótulo de saída esperado pelo -map
		segs = append(segs, fmt.Sprintf("[%s]null[vout]", label))
	}
	return strings.Join(segs, ";"), true
}

// segundos formata ms como "S.mmm" para os flags -ss/-t do ffmpeg e os tempos do enable.
func segundos(ms int) string {
	return fmt.Sprintf("%d.%03d", ms/1000, ms%1000)
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
