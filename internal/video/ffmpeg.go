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
// A queima está SUSPENSA por decisão do dono (ver LegendaQueimadaPadrao e a nota de
// suspensão na spec-12): o código continua aqui, desligado por flag, porque o que falta é
// o timestamp preciso (Rota D), não o desenho da legenda.
//
// Não altera o áudio/fala; a legenda vem da transcrição, sem reescrever palavras.
package video

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"srtclean/internal/harness"
	"srtclean/internal/pipeline"
	"srtclean/internal/processo"
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
	// Faixa inferior reservada à logo: a legenda fica ACIMA dela e a logo é centralizada
	// DENTRO dela (centro em H - faixa/2). Com a legenda suspensa, a faixa não reserva mais
	// nada contra o pregador — ela só define a que altura a logo se apoia. Calibrável por
	// flag (-faixa-logo) para gerar variantes sem recompilar.
	faixaLogoPadrao = 240
)

// Logo no rodapé (spec-13). Sobreposta (overlay do PNG com alpha) na faixa reservada,
// centralizada. Também uma faixa escura semitransparente no rodapé: garante que o texto
// BRANCO da logo seja legível mesmo sobre fundo claro (o rodapé deste vídeo é claro) e,
// de quebra, reforça a legenda. Tudo calibrável (flag/constante).
const (
	logoPathPadrao    = "assets/ibi_assinatura_shorts.png"
	logoLarguraPadrao = 550 // largura da logo no vídeo (px), aspecto preservado
	// Faixa escura do rodapé como GRADIENTE (transparente em cima → escuro embaixo),
	// suave como na arte de referência (não uma caixa de borda dura). A opacidade sobe com
	// uma curva (pow) para o começo ser IMPERCEPTÍVEL — sem linha visível no topo.
	//
	// 520/0.60 — ESCOLHA DO OPERADOR entre nove variantes medidas, quando a legenda foi
	// suspensa (spec-12). O gradiente existia para dois fins, contraste da legenda e
	// legibilidade da logo branca; sem legenda sobrou só a logo, que ocupa os 240 px de
	// baixo. O valor anterior, 1500 px, cobria 78% da altura do Short para servir uma faixa
	// de 240 px — era isso que dava a sensação de "apertado".
	//
	// Medido no mesmo frame (docs/medicoes/imagem-sem-legenda.md), com o pregador de camisa
	// BRANCA — o pior caso para a logo branca:
	//
	//   variante          torso (luma, ↑=mais imagem)   sob o texto da logo (luma, ↓=legível)
	//   1500/0.72 (antes)          87,96                        68,08
	//   520/0.60  <- escolhido    119,03                       113,29
	//   420/0.90                  120,16                        99,64
	//   480/1.00                  118,11                        83,04
	//   sem gradiente             122,33                       166,06
	//
	// O trade-off da escolha, explícito: rampa mais LONGA e menos OPACA (520/0.60) devolve
	// 100% da imagem que 420/0.90 devolveria (119,03 contra 120,16 — a diferença é
	// imperceptível) com um degradê mais suave, ao custo de 13 pontos de luma sob o texto da
	// logo (113,29 contra 99,64), ou seja MENOS contraste para o branco. Continua muito
	// melhor que sem gradiente nenhum (166,06, onde o texto quase desaparece na camisa) e
	// bem mais claro que os 68,08 de antes. A escolha é de aparência, e é do operador.
	//
	// As flags -rodape-altura/-rodape-escuro/-faixa-logo testam outros valores sem recompilar.
	rodapeAlturaPadrao = 520 // altura do gradiente (px), de baixo para cima
	easeGradiente      = 2.2 // expoente da curva de opacidade (>1 = começa mais suave)

	// Encode: medido, não arbitrado. Nitidez pela energia de altas frequências (laplaciano)
	// no mesmo trecho, com a cadeia de filtros de produção:
	//
	//   saída            quadro  legenda  tempo/short
	//   veryfast crf20    1.860    4.960     3,08 s   (era o default)
	//   veryfast crf18    1.887    4.980     3,25 s
	//   medium   crf20    1.921    5.031     5,29 s
	//   medium   crf18    1.931    5.038     5,25 s   <- escolhido
	//   slow     crf18    1.924    5.035    15,90 s
	//   (source 720p, antes de ampliar: 1.991)
	//
	// Duas conclusões que mudaram a escolha:
	//
	//  1. o PRESET domina o CRF: veryfast->medium rende +3,3%, crf20->18 rende +1,5%;
	//  2. `slow` NÃO rende nada sobre `medium` (1.924 contra 1.931, dentro do ruído) e custa
	//     3x o tempo. A hipótese inicial era slow/crf18; a medição parou em medium.
	//
	// O ganho total é modesto (+3,8% no quadro) e NÃO resolve a percepção de "imagem mole" —
	// essa vem da ampliação de 720p, não do encode. Mudou porque custa pouco: +2,2 s por Short,
	// ~9 s num pedido de quatro, contra ~86 s de download.
	presetPadrao = "medium"
	crfPadrao    = "18"
)

// RodapeAlphaPadrao é a opacidade máxima do gradiente do rodapé, EXPORTADA de propósito.
//
// Não pode ser um fallback interno para o campo zerado, porque zero tem significado próprio no
// contrato do Renderizador: "sem gradiente" (o cmd/render precisa disso, via -rodape-escuro 0).
// Como quem chama precisa passar o valor explicitamente, o valor tem de ser visível de fora —
// senão cada chamador escreve o seu, que é exatamente como o cmd/servidor acabou fixando 1.00 e
// tornando a constante letra morta.
//
// Quem monta um Renderizador para o caminho do operador usa esta constante. Um teste verifica
// que o valor chega ao comando do ffmpeg (internal/video/caminho_do_operador_test.go), porque
// conferir a constante não prova que ela é usada.
//
// 0.60 é a escolha do operador entre as variantes de rodapé sem legenda — ver
// rodapeAlturaPadrao acima, que é o par indissociável deste valor: opacidade só se julga
// junto com a altura da rampa (0.60 em 520 px é bem mais escuro no rodapé que 0.60 em
// 1500 px, porque a curva sobe no mesmo espaço).
const RodapeAlphaPadrao = 0.60

// LegendaQueimadaPadrao diz se o render QUEIMA a legenda na imagem do Short. Está em
// `false`: a queima está SUSPENSA, não removida (spec-12, seção "Suspensão temporária").
//
// Motivo: a legenda aparecia ADIANTADA. O carimbo de tempo que temos é o início do bloco
// do SRT, e toda palavra nova do bloco herda esse instante — erro medido de 2,5 a 3,4 s na
// última palavra (docs/medicoes/deslocamento-legenda.md). Legenda 3 s adiantada sobre a
// fala do pregador é pior que nenhuma legenda. A correção depende de alinhamento forçado
// (Rota D); quando ela existir, este default volta para `true`.
//
// A legenda CONTINUA insumo do pipeline: seleção (Fases 1–2), fronteiras de frase do corte
// (Fase 3), faixa de frases da tela de revisão e auditoria. O que está suspenso é só a
// queima na imagem.
//
// Exportada pelo mesmo motivo que RodapeAlphaPadrao: quem monta um Renderizador precisa
// poder referenciar o padrão em vez de escrever o seu (foi assim que o cmd/servidor
// fixou um 1.00 e tornou a constante do rodapé letra morta).
const LegendaQueimadaPadrao = false

// Executor roda um comando externo e devolve stdout, stderr e o erro de execução.
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

	// Legenda queimada (spec-12). SUSPENSA: o zero-value (false) é o estado atual de
	// produção — sem queima. Quem quer queimar liga explicitamente. Ver
	// LegendaQueimadaPadrao para o motivo e a condição de volta.
	Legenda bool

	// Calibração da legenda (spec-12); zero/"" usa os defaults acima. Só têm efeito com
	// Legenda = true.
	FontePath     string // caminho do .ttf da fonte
	TamanhoFonte  int    // px
	CharsPorLinha int    // largura da linha (ritmo de troca dos blocos)

	// Logo e rodapé (spec-13); zero/"" usa os defaults acima.
	LogoPath     string  // PNG da logo; se o arquivo não existir, renderiza sem logo
	LogoLargura  int     // largura da logo no vídeo (px)
	LogoAjusteY  int     // ajuste vertical da logo a partir do centro da faixa (px; + desce)
	RodapeAlpha  float64 // opacidade do gradiente escuro na base (0 = sem gradiente)
	RodapeAltura int     // altura do gradiente escuro (px)
	FaixaLogo    int     // altura da faixa em que a logo é centralizada (px; 0 = default)

	// Preset/CRF do x264. Vazios usam presetPadrao/crfPadrao. Configuráveis porque a
	// escolha é um trade-off medido (tempo x nitidez), e medir exige variar.
	Preset string
	CRF    string
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

// presetOu/crfOu aplicam o padrão para quem passa vazio — em um lugar só, para o método do
// Renderizador e a função livre nunca divergirem.
func presetOu(p string) string {
	if p == "" {
		return presetPadrao
	}
	return p
}

func crfOu(c string) string {
	if c == "" {
		return crfPadrao
	}
	return c
}

func (r *Renderizador) preset() string { return presetOu(r.Preset) }
func (r *Renderizador) crf() string    { return crfOu(r.CRF) }

func (r *Renderizador) rodapeAltura() int {
	if r.RodapeAltura <= 0 {
		return rodapeAlturaPadrao
	}
	return r.RodapeAltura
}

func (r *Renderizador) faixaLogo() int {
	if r.FaixaLogo <= 0 {
		return faixaLogoPadrao
	}
	return r.FaixaLogo
}

// Renderizar gera um Short por candidato, em ordem de score (maior primeiro), e
// devolve os caminhos gerados. Em falha, seta Status=erro e Erro. Os candidatos vêm
// SEMPRE de fora (spec-09: fonte única = arquivo de seleção validado); o pedido não
// os carrega mais.
// Renderizar renderiza os candidatos usando a origem de tempo DECLARADA no pedido
// (pipeline.Pedido.OrigemMs) — o instante absoluto do vídeo original a que o t=0 do
// video.mp4 corresponde. Quem baixou o arquivo declarou; aqui só se lê.
//
// Antes esta função SUPUNHA ped.Inicio. Estava certo para o vídeo baixado por janela
// (cmd/baixar) e errado para o vídeo inteiro do servidor, cujo pedido.json também tem um
// Inicio real (o início da pregação): `cmd/render -id <pedido do servidor>` produzia Shorts
// da cena errada, deslocados pelo Inicio, com a duração CORRETA — sem nenhum sinal de erro.
// Se a origem não estiver declarada, falha com mensagem que diz o que fazer, em vez de
// escolher um padrão (ver Pedido.Origem).
func (r *Renderizador) Renderizar(ctx context.Context, ped *pipeline.Pedido, candidatos []validacao.Candidato) ([]string, error) {
	origemMs, err := ped.Origem()
	if err != nil {
		ped.Status = pipeline.EstadoErro
		ped.Erro = err.Error()
		return nil, err
	}
	return r.RenderizarComOrigem(ctx, ped, candidatos, origemMs)
}

// RenderizarComOrigem é o mesmo render, mas com a ORIGEM DE TEMPO recebida por PARÂMETRO em
// vez de lida do pedido: origemMs é o instante ABSOLUTO (no vídeo do YouTube) que corresponde
// ao t=0 do arquivo video.mp4.
//
// É o que a fase pesada do servidor (spec-05) usa, porque lá a origem vem do BAIXADOR (valor
// devolvido por BaixarVideoCompleto) e o servidor a repassa direto, além de gravá-la no
// pedido.json. O corte de cada candidato é SEMPRE (start - origemMs).
//
// De onde vem a origem, hoje, em cada caminho:
//
//	cmd/baixar + cmd/render   janela [inicio, fim]  ->  origem = inicio  (pedido.json)
//	servidor (fase pesada)    vídeo inteiro         ->  origem = 0       (do baixador)
//
// NÃO deduza a origem de ped.Inicio. Era o que o Renderizar fazia, e é a origem de um bug
// real: no caminho do servidor, ped.Inicio é o início da PREGAÇÃO e o arquivo é o vídeo
// inteiro, então o corte saía deslocado pelo Inicio — com a duração correta e a cena errada.
// O fato mora em pipeline.Pedido.OrigemMs; ver spec-09.
//
// (Nota histórica: até 2026-07 a fase pesada baixava a "janela contígua" [menor start
// aprovado, maior end aprovado] e a origem era esse menor start, calculado. Isso deixou de
// existir quando o download passou a ser do vídeo inteiro — ~79x mais rápido. Se você
// encontrar menção a janela contígua em outro comentário, está desatualizada.)
func (r *Renderizador) RenderizarComOrigem(ctx context.Context, ped *pipeline.Pedido, candidatos []validacao.Candidato, origemMs int) ([]string, error) {
	paths, err := r.renderizar(ctx, ped, candidatos, origemMs)
	if err != nil {
		ped.Status = pipeline.EstadoErro
		ped.Erro = err.Error()
		return nil, err
	}
	return paths, nil
}

func (r *Renderizador) renderizar(ctx context.Context, ped *pipeline.Pedido, candidatos []validacao.Candidato, origemMs int) ([]string, error) {
	if len(candidatos) == 0 {
		return nil, fmt.Errorf("nenhum candidato para renderizar")
	}

	trabDir := filepath.Join(r.baseDir(), ped.ID)
	videoPath := filepath.Join(trabDir, "video.mp4")

	// Texto LIMPO da legenda: vem da transcrição já limpa (mesma que a seleção usa),
	// passada pela desduplicação/segmentação da Fase 3 (harness.Frasear). NÃO usamos o
	// SRT bruto rolling (spec-12).
	//
	// Com a queima SUSPENSA (Legenda = false, o padrão de hoje) nem lemos a transcrição:
	// sem frases, BlocosLegenda devolve nada e nenhum drawtext entra no filtro. O aviso no
	// stderr existe para "Short sem legenda" nunca ser confundido com falha silenciosa.
	var frases []harness.Frase
	if r.Legenda {
		transcPath := filepath.Join(trabDir, "transcricao.txt")
		transcBytes, err := os.ReadFile(transcPath)
		if err != nil {
			return nil, fmt.Errorf("lendo transcrição %q (necessária p/ a legenda limpa): %w", transcPath, err)
		}
		frases = harness.Frasear(string(transcBytes))
	} else {
		fmt.Fprintln(os.Stderr, "render: legenda queimada DESLIGADA (spec-12 suspensa; -legenda para ligar)")
	}

	est := EstiloLegenda{
		FontePath:    r.fontePath(),
		Tamanho:      r.tamanhoFonte(),
		Contorno:     contornoLegenda,
		Sombra:       sombraLegenda,
		EspacoLinhas: espacoLinhasLegenda,
		FaixaLogoPx:  r.faixaLogo(),
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
	logo := LogoConfig{Path: logoPath, LarguraPx: r.logoLargura(), AjusteY: r.LogoAjusteY}

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

	// Os blocos de legenda viram arquivos .txt temporários (short_NN.subNNN.txt) que o
	// drawtext lê via textfile= (evita escapar o texto no filtro). São descartáveis: o
	// ffmpeg já os leu quando o Short fica pronto. Removemos todos ao final (antes,
	// acumulavam órfãos na pasta de trabalho — 46 por pedido).
	var tempTxt []string
	defer func() {
		for _, f := range tempTxt {
			os.Remove(f)
		}
	}()

	var paths []string
	for i, cand := range ordenados {
		startMs, ok1 := transcricao.HmsToMs(cand.Start)
		endMs, ok2 := transcricao.HmsToMs(cand.End)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("candidato %d com tempos inválidos: start=%q end=%q", i+1, cand.Start, cand.End)
		}

		// Corte relativo ao video.mp4, que começa em t=0 na origem DECLARADA por quem baixou o
		// arquivo (janela do cmd/baixar: o inicio; vídeo inteiro do servidor: 0). Ver
		// RenderizarComOrigem e pipeline.Pedido.OrigemMs.
		cutStartMs := startMs - origemMs
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
			tempTxt = append(tempTxt, tf)
			usados = append(usados, bl)
			textfiles = append(textfiles, tf)
		}
		filtro, complexo := montarFiltro(usados, textfiles, est, comLogo, logo, grad)

		outPath := filepath.Join(outDir, fmt.Sprintf("short_%02d.mp4", i+1))
		logoInput := ""
		if comLogo {
			logoInput = logo.Path
		}
		args := ArgsFFmpeg(videoPath, logoInput, outPath, cutStartMs, durMs, filtro, complexo, r.preset(), r.crf())

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
func ArgsFFmpeg(videoPath, logoPath, outPath string, cutStartMs, durMs int, filtro string, complexo bool, preset, crf string) []string {
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
		"-c:v", "libx264", "-preset", presetOu(preset), "-crf", crfOu(crf),
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
	Path      string // PNG com alpha
	LarguraPx int    // largura no vídeo (aspecto preservado)
	AjusteY   int    // ajuste vertical a partir do centro da faixa (px; + desce, - sobe)
}

// GradConfig é o gradiente escuro do rodapé (spec-13): transparente em cima → escuro
// embaixo, para dar legibilidade à logo/legenda sobre fundo claro, de forma suave.
type GradConfig struct {
	Altura int     // altura do gradiente (px), medido do fundo para cima
	Alpha  float64 // opacidade máxima (na base); 0 = sem gradiente
}

func (g GradConfig) ativo() bool { return g.Alpha > 0 && g.Altura > 0 }

// filtroBase é o reenquadramento comum: crop central 9:16 + scale para 1080x1920.
//
// flags=lanczos, não o bicúbico padrão do swscale: aqui SEMPRE ampliamos. A transmissão da
// igreja é 720p, e o corte 9:16 do centro rende só 405x720 pixels reais, esticados para
// 1080x1920 — ~2,7x em área, mais da metade dos pixels do Short é interpolada. O bicúbico
// é mais macio nessa ampliação (borra traços finos, especialmente o rosto); o lanczos
// preserva mais detalhe, ao custo desprezível no tempo de render (medido). Ver a nota sobre
// a limitação de origem na spec-05: o teto de qualidade é a transmissão, não o pipeline.
func filtroBase() string {
	return fmt.Sprintf("crop=ih*9/16:ih,scale=%d:%d:flags=lanczos,setsar=1", larguraSaida, alturaSaida)
}

// drawtextFiltros (lógica pura) monta a cadeia de drawtext (um por bloco de legenda),
// juntada por vírgula, sem vírgula inicial (vazio se não há blocos). Cada drawtext carrega
// a fonte direto do .ttf, branca com contorno/sombra, centralizada, ancorada na BASE acima
// da faixa da logo, visível só na janela do bloco. Texto vem de arquivo (sem escaping).
//
// A janela usa limite superior EXCLUSIVO — `gte(t,a)*lt(t,b)` em vez de between(t,a,b),
// que é inclusivo nos dois extremos (spec-12). Como blocos vizinhos compartilham a
// fronteira (fim de um = início do outro), o between fazia os dois aparecerem no frame
// exato da fronteira (legenda duplicada/borrada). Com lt(t,b), no instante `b` só o bloco
// seguinte (gte) fica ativo — nunca os dois.
func drawtextFiltros(blocos []BlocoLegenda, textfiles []string, est EstiloLegenda) string {
	var fs []string
	for i, bl := range blocos {
		fs = append(fs, fmt.Sprintf(
			"drawtext=fontfile=%s:textfile=%s:expansion=none"+
				":fontsize=%d:fontcolor=white:borderw=%d:bordercolor=black"+
				":shadowcolor=black@0.55:shadowx=%d:shadowy=%d:line_spacing=%d:text_align=C"+
				":x=(w-text_w)/2:y=h-%d-text_h:enable='gte(t,%s)*lt(t,%s)'",
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
		// Logo CENTRALIZADA (horizontal e vertical) na faixa reservada — o espaço entre a
		// linha da legenda (topo da faixa, em H-faixa) e o fim do vídeo (spec-13). Centro
		// da faixa = H - faixa/2; topo da logo = centro - h/2. AjusteY desce (+) ou sobe (-).
		segs = append(segs, fmt.Sprintf("[1:v]scale=%d:-2[logo]", logo.LarguraPx))
		segs = append(segs, fmt.Sprintf("[%s][logo]overlay=x=(W-w)/2:y=H-%d/2-h/2+%d[vout]", label, est.FaixaLogoPx, logo.AjusteY))
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
