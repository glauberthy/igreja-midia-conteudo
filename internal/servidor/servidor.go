// Pacote servidor é a interface web local do operador leigo (spec-05). Serve uma
// única página (HTMX + HTML/CSS, sem framework/build) numa porta local sem auth, e
// orquestra o FLUXO INVERTIDO: primeiro a fase leve (baixar SÓ a legenda → rodar a
// seleção), o operador revisa/aprova, e só então a fase pesada (baixar vídeo + render).
//
// Esta é a Parte 1 da spec-05: servidor + fase leve. Player do YouTube, aprovar/
// reprovar e a fase pesada entram nas Partes 2 e 3.
//
// Testabilidade: o download da legenda e a seleção são injetados por interface
// (BaixadorLegenda, Selecionador), então os testes exercitam as rotas e a máquina de
// estados com mocks — sem subir yt-dlp nem o modelo.
package servidor

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"srtclean/internal/download"
	"srtclean/internal/pipeline"
	"srtclean/internal/retencao"
	"srtclean/internal/transcricao"
	"srtclean/internal/validacao"
)

//go:embed templates.html
var arquivos embed.FS

var tmpl = template.Must(template.ParseFS(arquivos, "templates.html"))

// BaixadorLegenda baixa SÓ a legenda (sem o vídeo) e gera a transcrição na pasta de
// trabalho do pedido. É a fase leve do fluxo invertido.
type BaixadorLegenda interface {
	BaixarLegenda(ctx context.Context, ped *pipeline.Pedido) error
}

// Selecionador roda a seleção (harness) sobre a transcrição e devolve os candidatos
// já validados.
type Selecionador interface {
	Selecionar(ctx context.Context, transcricaoPath string) ([]validacao.Candidato, error)
}

// BaixadorVideo baixa o vídeo do pedido para a fase pesada (spec-05 parte 3). Baixa o
// vídeo INTEIRO com o downloader nativo paralelo — medido como ~79x mais rápido que baixar
// só a janela dos aprovados (o gargalo é paralelismo, não volume; ver spec-05).
type BaixadorVideo interface {
	BaixarVideoCompleto(ctx context.Context, ped *pipeline.Pedido) error
}

// RenderizadorVideo renderiza os candidatos aprovados a partir do video.mp4 baixado, com a
// ORIGEM DE TEMPO explícita (origemMs = instante absoluto que corresponde ao t=0 do
// arquivo). Devolve os caminhos dos Shorts gerados.
type RenderizadorVideo interface {
	RenderizarComOrigem(ctx context.Context, ped *pipeline.Pedido, cands []validacao.Candidato, origemMs int) ([]string, error)
}

// registro é o estado de um pedido na memória do servidor. Os candidatos ficam aqui
// (o Pedido não os carrega — spec-09); são também persistidos em disco. aprovados
// guarda os índices que o operador aprovou (spec-05 parte 2), para a fase pesada (parte 3).
type registro struct {
	ped       *pipeline.Pedido
	cands     []validacao.Candidato
	textos    []string // texto REALMENTE falado na janela de cada candidato (revisão)
	aprovados []int
	// ajustes guarda, por índice de candidato, o corte que o operador refez à mão
	// (spec-05 v2). São ESTES tempos que vão ao render — ver candidatosAprovados.
	ajustes  map[int]TrechoAjustado
	shorts   []string  // nomes dos arquivos finais gerados (fase pesada), para download
	metricas *Metricas // tempos por etapa (auditoria de desempenho)
}

// Servidor guarda as dependências e o registro em memória dos pedidos.
type Servidor struct {
	baixador         BaixadorLegenda
	selecionador     Selecionador
	baixadorVideo    BaixadorVideo
	renderizador     RenderizadorVideo
	baseDir          string
	outDir           string
	logRodadasPath   string
	temposPath       string
	assetsDir        string
	reterPedidos     int
	limpezaDesligada bool
	logTemposFn      func(string)
	prazos           Prazos
	agora            func() time.Time
	gerarID          func() string
	mux              *http.ServeMux

	mu      sync.Mutex
	pedidos map[string]*registro
	seq     int

	logMu sync.Mutex // serializa a escrita do log de rodadas
}

// Opcoes configura o Servidor. Campos zero recebem padrões (agora=time.Now,
// gerarID=timestamp+sequência).
type Opcoes struct {
	Baixador      BaixadorLegenda
	Selecionador  Selecionador
	BaixadorVideo BaixadorVideo     // fase pesada (spec-05 parte 3); nil desabilita
	Renderizador  RenderizadorVideo // fase pesada; nil desabilita
	BaseDir       string
	OutDir        string // raiz dos Shorts finais (padrão "finalizados")
	// LogRodadasPath é onde cada seleção é registrada como uma "rodada" (avaliação de
	// variância). Vazio usa o padrão "resultados/rodadas.md".
	LogRodadasPath string
	// TemposPath é o CSV de auditoria de desempenho (uma linha por pedido, append).
	// Vazio usa o padrão "resultados/tempos.csv".
	TemposPath string
	// ReterPedidos é quantos pedidos mantêm o material bruto após a limpeza automática
	// (spec-06). 0 usa o padrão 1 (o último, para regerar sem baixar de novo).
	// LimpezaDesligada desativa a limpeza automática (o cmd/limpar continua disponível).
	ReterPedidos     int
	LimpezaDesligada bool
	// AssetsDir é a pasta servida em /assets/ (fonte da identidade, logo). Vazio = "assets".
	AssetsDir string
	// LogTempos recebe o resumo de desempenho/limpeza de cada pedido. Nil = stderr.
	LogTempos func(string)
	// Prazos limita a duração de cada etapa, garantindo que o pedido SEMPRE termina.
	// Campos zerados usam PrazosPadrao(). Ver prazos.go.
	Prazos  Prazos
	Agora   func() time.Time // injetável para testes
	GerarID func() string    // injetável para testes
}

// Novo cria o servidor e registra as rotas.
func Novo(o Opcoes) *Servidor {
	s := &Servidor{
		baixador:         o.Baixador,
		selecionador:     o.Selecionador,
		baixadorVideo:    o.BaixadorVideo,
		renderizador:     o.Renderizador,
		baseDir:          o.BaseDir,
		outDir:           o.OutDir,
		logRodadasPath:   o.LogRodadasPath,
		temposPath:       o.TemposPath,
		assetsDir:        o.AssetsDir,
		reterPedidos:     o.ReterPedidos,
		limpezaDesligada: o.LimpezaDesligada,
		logTemposFn:      o.LogTempos,
		prazos:           o.Prazos.comPadroes(),
		agora:            o.Agora,
		gerarID:          o.GerarID,
		pedidos:          make(map[string]*registro),
		mux:              http.NewServeMux(),
	}
	if s.baseDir == "" {
		s.baseDir = "trabalho"
	}
	if s.outDir == "" {
		s.outDir = "finalizados"
	}
	if s.logRodadasPath == "" {
		s.logRodadasPath = filepath.Join("resultados", "rodadas.md")
	}
	if s.temposPath == "" {
		s.temposPath = filepath.Join("resultados", "tempos.csv")
	}
	if s.assetsDir == "" {
		s.assetsDir = "assets"
	}
	if s.agora == nil {
		s.agora = time.Now
	}
	if s.gerarID == nil {
		s.gerarID = s.idPadrao
	}
	s.mux.HandleFunc("GET /", s.handleIndex)
	s.mux.HandleFunc("POST /pedidos", s.handleCriar)
	s.mux.HandleFunc("GET /pedidos/{id}", s.handleStatus)
	s.mux.HandleFunc("POST /pedidos/{id}/aprovar", s.handleAprovar)
	s.mux.HandleFunc("POST /pedidos/{id}/ajustar", s.handleAjustar)
	s.mux.HandleFunc("GET /finalizados/{id}/{arquivo}", s.handleBaixarFinal)
	// Assets estáticos (fonte da identidade, logo) — servidos do disco.
	s.mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir(s.assetsDir))))
	return s
}

// ServeHTTP torna o Servidor um http.Handler (útil para httptest).
func (s *Servidor) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// idPadrao gera um id legível e único por instância: web-<AAAAMMDD-HHMMSS>-<seq>.
func (s *Servidor) idPadrao() string {
	s.seq++
	return fmt.Sprintf("web-%s-%d", s.agora().Format("20060102-150405"), s.seq)
}

func (s *Servidor) handleIndex(w http.ResponseWriter, r *http.Request) {
	// GET / só responde a "/", não a qualquer prefixo (evita casar rotas removidas).
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "pagina", nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// entrada representa os campos do formulário/JSON de criação de pedido.
type entrada struct {
	YouTubeURL string `json:"youtube_url"`
	Inicio     string `json:"inicio"`
	Fim        string `json:"fim"`
}

func (s *Servidor) handleCriar(w http.ResponseWriter, r *http.Request) {
	ent, err := lerEntrada(r)
	if err != nil {
		s.responderErroCriacao(w, r, "não entendi os dados enviados")
		return
	}
	if msg, ok := validarEntrada(ent); !ok {
		s.responderErroCriacao(w, r, msg)
		return
	}

	id := s.gerarID()
	ped := pipeline.NovoPedido(id, ent.YouTubeURL, ent.Inicio, ent.Fim, s.agora())
	met := &Metricas{ID: id, DuracaoSermaoS: duracaoJanelaS(ent.Inicio, ent.Fim)}
	met.IniciarPedido(s.agora())
	reg := &registro{ped: ped, metricas: met}
	s.mu.Lock()
	s.pedidos[id] = reg
	s.mu.Unlock()

	// Dispara a fase leve em background e responde o id na hora (não bloqueia).
	go s.faseLeve(reg)

	if querJSON(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"id": id})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.ExecuteTemplate(w, "resultado", struct{ ID string }{ID: id})
}

// responderErroCriacao devolve o erro de validação: 400 + JSON para clientes de API,
// ou um fragmento HTML (código 200 para o HTMX conseguir trocar #resultado) no navegador.
func (s *Servidor) responderErroCriacao(w http.ResponseWriter, r *http.Request, msg string) {
	if querJSON(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"erro": msg})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<p class="erro">%s</p>`, template.HTMLEscapeString(msg))
}

func (s *Servidor) handleStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	reg, ok := s.pedidos[id]
	var vis visaoStatus
	if ok {
		vis = montarVisao(reg)
	}
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	if querJSON(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(vis.json())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.ExecuteTemplate(w, "status", vis)
}

// handleAprovar recebe os índices aprovados pelo operador (spec-05 parte 2), valida-os
// contra os candidatos do pedido, registra a decisão e move o pedido para
// aguardando-processamento. A fase pesada (baixar vídeo + render) é a Parte 3.
func (s *Servidor) handleAprovar(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	reg, ok := s.pedidos[id]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	// Só faz sentido aprovar depois da seleção concluída.
	s.mu.Lock()
	pronto := reg.ped.Status == pipeline.EstadoAguardandoAprovacao ||
		reg.ped.Status == pipeline.EstadoAguardandoProcessamento
	nCands := len(reg.cands)
	s.mu.Unlock()
	if !pronto {
		s.responderErroAprovar(w, r, http.StatusConflict, "a seleção ainda não terminou — aguarde os candidatos")
		return
	}

	indices, ajustesRecebidos, err := lerAprovados(r)
	if err != nil {
		s.responderErroAprovar(w, r, http.StatusBadRequest, "não entendi a lista de aprovados")
		return
	}
	limpos, ok := validarIndices(indices, nCands)
	if !ok {
		s.responderErroAprovar(w, r, http.StatusBadRequest, "índices de aprovação fora do intervalo dos candidatos")
		return
	}

	// Recalcula cada ajuste no SERVIDOR e recusa a aprovação se algum trecho ajustado
	// estiver fora da faixa válida. Recusar aqui, e não no cliente, é o que garante a
	// guarda: um POST direto (ou um JS desatualizado) não pode enfiar um corte de 64 s no
	// render. A mensagem diz qual trecho e o que falta.
	ajustes, motivo := s.validarAjustes(reg, limpos, ajustesRecebidos)
	if motivo != "" {
		s.responderErroAprovar(w, r, http.StatusBadRequest, motivo)
		return
	}

	s.mu.Lock()
	reg.aprovados = limpos
	reg.ajustes = ajustes
	// Fecha o tempo de ESPERA HUMANA (revisão do operador). Fica em coluna própria e fora
	// do total de máquina — é tempo de pessoa, não de sistema.
	if reg.metricas.AguardandoMs == 0 {
		reg.metricas.AguardandoMs = reg.metricas.marcar(s.agora())
	}
	reg.metricas.NumAprovados = len(limpos)
	reg.ped.Status = pipeline.EstadoAguardandoProcessamento
	vis := montarVisao(reg)
	s.mu.Unlock()

	// Dispara a fase pesada (baixar vídeo dos aprovados + render) em background, se houver
	// pelo menos um aprovado e as dependências estiverem ligadas (parte 3). O status volta
	// a "em progresso", então a página retoma o polling (baixando-video → renderizando →
	// concluido). Sem aprovados, não há o que gerar.
	if len(limpos) > 0 && s.baixadorVideo != nil && s.renderizador != nil {
		go s.faseHeavy(reg)
	}

	if querJSON(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]any{
			"id": id, "status": string(pipeline.EstadoAguardandoProcessamento), "aprovados": limpos,
		})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.ExecuteTemplate(w, "status", vis)
}

// faseHeavy é a máquina de estados da fase pesada (spec-05 parte 3): baixando-video →
// renderizando → concluido (ou erro, com mensagem clara — nunca trava). Roda em goroutine.
//
// Alinhamento de tempo: baixa o vídeo INTEIRO (downloader nativo paralelo), então o arquivo
// começa no início do vídeo e a origem é ZERO — o corte de cada trecho usa o start/end
// ABSOLUTO, sem nenhum cálculo de origem a propagar. Isso substituiu a janela contígua
// (origem = menor start): além de ~79x mais rápido (577 s → 7,3 s, medido), elimina a
// classe de bug de "origem trocada" entre download e render.
func (s *Servidor) faseHeavy(reg *registro) {
	ctx := context.Background()

	s.mu.Lock()
	aprovados := candidatosAprovados(reg)
	idPedido := reg.ped.ID
	s.mu.Unlock()
	if len(aprovados) == 0 {
		s.setErro(reg, "nenhum trecho aprovado para gerar")
		return
	}

	// Espaço em disco ANTES de baixar (spec-06): um vídeo de culto passa de 900 MB. Se
	// faltar margem, tenta a limpeza automática; se ainda faltar, falha AQUI com números —
	// muito melhor que o disco encher no meio do download (o yt-dlp morre com erro de
	// biblioteca, que não diz nada ao operador).
	if err := s.garantirEspaco(reg.ped.ID); err != nil {
		s.setErro(reg, err.Error())
		return
	}

	s.setStatus(reg, pipeline.EstadoBaixandoVideo)
	// Cópia: o Baixador escreve Status/Erro no Pedido (contrato do cmd/baixar, onde não há
	// concorrência). Aqui o registro é compartilhado com o handleStatus, então deixá-lo
	// escrever direto é corrida de verdade (pega pelo -race). O servidor é dono do status.
	// Progresso, não tempo total: ver Prazos.VideoSemProgresso. O tamanho do culto varia
	// demais (994 MB visto; 2h dariam ~1,8 GB) para um teto fixo ter margem honesta.
	dirPedido := filepath.Join(s.baseDir, idPedido)
	err := etapaComProgresso(ctx, "o download do vídeo", dirPedido,
		s.prazos.VideoSemProgresso, s.prazos.VideoTeto,
		func(ctx context.Context) error {
			return s.baixadorVideo.BaixarVideoCompleto(ctx, s.copiaPedido(reg))
		})
	if err != nil {
		s.setErro(reg, mensagemErroDownload(err))
		return
	}
	videoPath := filepath.Join(dirPedido, "video.mp4")
	s.metrica(reg, func(m *Metricas) {
		m.BaixarVideoMs = m.marcar(s.agora())
		m.BytesVideo = tamanhoArquivo(videoPath)
	})

	s.setStatus(reg, pipeline.EstadoRenderizando)
	// Origem 0: o video.mp4 é o vídeo inteiro, então t=0 do arquivo == t=0 do vídeo.
	var paths []string
	err = etapaComPrazo(ctx, "a renderização", s.prazos.Renderize, func(ctx context.Context) error {
		var e error
		paths, e = s.renderizador.RenderizarComOrigem(ctx, s.copiaPedido(reg), aprovados, origemVideoCompleto)
		return e
	})
	if err != nil {
		s.setErro(reg, comPrefixo("falha ao renderizar os Shorts: ", err))
		return
	}

	nomes := make([]string, len(paths))
	for i, p := range paths {
		nomes[i] = filepath.Base(p)
	}
	s.mu.Lock()
	reg.shorts = nomes
	reg.metricas.RenderizarMs = reg.metricas.marcar(s.agora())
	reg.ped.Status = pipeline.EstadoConcluido
	s.mu.Unlock()

	// Auditoria de desempenho: resumo no log + uma linha no CSV histórico.
	s.finalizarPedido(reg, "")

	// Limpeza de disco (spec-06): concluído o pedido, apaga o bruto dos ANTERIORES. Sem
	// isto, ~571 MB por pedido se acumulam até o disco encher — e o operador recebe um
	// erro incompreensível, longe do problema real. O pedido atual é intocável (ainda pode
	// ser regerado sem baixar de novo).
	s.limparAntigos(reg.ped.ID)
}

// limparAntigos roda a política de retenção. Ver limparSobLock para a invariante.
func (s *Servidor) limparAntigos(idAtual string) {
	s.limparSobLock(idAtual)
}

// intocaveisLocked lista os pedidos que a limpeza NÃO pode enxergar: todo pedido que o
// servidor conhece e que ainda não chegou a estado terminal. Exige s.mu já segurado —
// é o mesmo mutex que registra pedido novo e muda estado, então a lista não pode
// envelhecer entre ser calculada e ser usada.
func (s *Servidor) intocaveisLocked(extras ...string) []string {
	ids := append([]string(nil), extras...)
	for id, reg := range s.pedidos {
		if !estadoTerminal(reg.ped.Status) {
			ids = append(ids, id)
		}
	}
	return ids
}

// limparSobLock executa a limpeza com o mutex SEGURADO durante a decisão E a remoção.
// A limpeza apaga arquivo: uma corrida aqui não corrompe uma leitura, ela destrói o
// video.mp4 que um pedido acabou de baixar, em silêncio. Segurar o mutex do começo ao fim
// fecha a janela por construção — nenhum pedido nasce nem avança nesse intervalo. Custo
// nulo na prática: a remoção é de poucos arquivos e o servidor atende um pedido por vez.
func (s *Servidor) limparSobLock(extras ...string) {
	if s.limpezaDesligada {
		return
	}
	s.mu.Lock()
	res, err := retencao.Limpar(retencao.Opcoes{
		RaizTrabalho: s.baseDir,
		Reter:        s.reterPedidos,
		Intocaveis:   s.intocaveisLocked(extras...),
	})
	s.mu.Unlock()

	if err != nil {
		fmt.Fprintf(os.Stderr, "aviso: limpeza de disco falhou: %v\n", err)
		return
	}
	if res.BytesLiberados > 0 {
		s.logTempos(res.Resumo())
	}
}

// estadoTerminal diz se o pedido acabou (para o bem ou para o mal). Só o que terminou
// pode ter o material bruto limpo.
func estadoTerminal(e pipeline.Estado) bool {
	return e == pipeline.EstadoConcluido || e == pipeline.EstadoErro
}

// limparResiduoDeErro apaga o material bruto do pedido que FALHOU. Ele não tem Short a
// regerar, e um download interrompido deixa mp4 parcial, .part e .ytdl. Como a falha
// costuma acontecer justamente com o disco apertado, não limpar aqui realimentaria o
// problema: falhas acumulariam lixo e nunca seriam limpas.
func (s *Servidor) limparResiduoDeErro(id string) {
	if s.limpezaDesligada || id == "" {
		return
	}
	s.mu.Lock()
	p, err := retencao.LimparPedido(s.baseDir, id, false)
	s.mu.Unlock()
	if err != nil {
		fmt.Fprintf(os.Stderr, "aviso: limpeza do resíduo de %s falhou: %v\n", id, err)
		return
	}
	if p.Bytes > 0 {
		s.logTempos(fmt.Sprintf("limpeza: resíduo do pedido %s removido (%s em %d arquivo(s))",
			id, retencao.FormatarBytes(p.Bytes), len(p.Arquivos)))
	}
}

// garantirEspaco confere a margem de disco antes da fase pesada; se apertar, tenta a
// limpeza (preservando o pedido em curso) antes de desistir.
func (s *Servidor) garantirEspaco(idAtual string) error {
	if s.limpezaDesligada {
		return nil // sem limpeza automática, não faz sentido bloquear por espaço
	}
	// GarantirEspaco também APAGA quando falta margem — mesma invariante do limparSobLock.
	s.mu.Lock()
	_, err := retencao.GarantirEspaco(retencao.Opcoes{
		RaizTrabalho: s.baseDir,
		Reter:        s.reterPedidos,
		Intocaveis:   s.intocaveisLocked(idAtual),
	}, retencao.MargemPadrao)
	s.mu.Unlock()
	return err
}

// tokensAprox estima o tamanho da transcrição em tokens (bytes/4 — regra prática boa o
// bastante para explicar variação de tempo entre sermões). 0 se o arquivo não abrir.
func tokensAprox(path string) int {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return int(fi.Size() / 4)
}

// tamanhoArquivo devolve o tamanho em bytes (0 se não existir).
func tamanhoArquivo(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// handleBaixarFinal serve um Short gerado (finalizados/<id>/<arquivo>) para download. Só
// serve arquivos que o pedido realmente gerou (whitelist reg.shorts) — evita travessia de
// caminho e vazamento de arquivos de outros pedidos.
func (s *Servidor) handleBaixarFinal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	arquivo := r.PathValue("arquivo")
	s.mu.Lock()
	reg, ok := s.pedidos[id]
	permitido := false
	if ok {
		for _, n := range reg.shorts {
			if n == arquivo {
				permitido = true
				break
			}
		}
	}
	s.mu.Unlock()
	if !ok || !permitido {
		http.NotFound(w, r)
		return
	}
	// arquivo é um nome da whitelist (sem separadores); Base é defesa extra.
	http.ServeFile(w, r, filepath.Join(s.outDir, id, filepath.Base(arquivo)))
}

func (s *Servidor) responderErroAprovar(w http.ResponseWriter, r *http.Request, code int, msg string) {
	if querJSON(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]string{"erro": msg})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	fmt.Fprintf(w, `<p class="erro">%s</p>`, template.HTMLEscapeString(msg))
}

// lerAprovados extrai os índices aprovados de JSON ({"aprovados":[..]}) ou do formulário
// (campos repetidos aprovados=0&aprovados=2, como o HTMX serializa os checkboxes).
// ajusteRecebido é um corte manual como chega no POST /aprovar. Só tempos: o servidor
// RECALCULA hook, duração e texto a partir deles, em vez de confiar no que o cliente
// mandou. O cliente é conveniência; a fonte de verdade é o recálculo.
type ajusteRecebido struct {
	Indice int     `json:"indice"`
	Start  float64 `json:"start"`
	End    float64 `json:"end"`
}

func lerAprovados(r *http.Request) ([]int, []ajusteRecebido, error) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		defer r.Body.Close()
		var corpo struct {
			Aprovados []int            `json:"aprovados"`
			Ajustes   []ajusteRecebido `json:"ajustes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&corpo); err != nil {
			return nil, nil, err
		}
		return corpo.Aprovados, corpo.Ajustes, nil
	}
	if err := r.ParseForm(); err != nil {
		return nil, nil, err
	}
	var out []int
	for _, s := range r.Form["aprovados"] {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return nil, nil, err
		}
		out = append(out, n)
	}
	// Formulário sem JS: "ajuste_<i>=start,end" (mesma semântica do caminho JSON).
	ajs, err := lerAjustesDoForm(r)
	if err != nil {
		return nil, nil, err
	}
	return out, ajs, nil
}

// lerAjustesDoForm aceita os ajustes no POST de formulário, para a tela seguir funcionando
// sem JavaScript (o cliente novo usa JSON).
func lerAjustesDoForm(r *http.Request) ([]ajusteRecebido, error) {
	var out []ajusteRecebido
	for chave, vals := range r.Form {
		if !strings.HasPrefix(chave, "ajuste_") || len(vals) == 0 {
			continue
		}
		idx, err := strconv.Atoi(strings.TrimPrefix(chave, "ajuste_"))
		if err != nil {
			return nil, err
		}
		partes := strings.Split(vals[0], ",")
		if len(partes) != 2 {
			return nil, fmt.Errorf("ajuste %q: esperado \"start,end\"", chave)
		}
		ini, err1 := strconv.ParseFloat(strings.TrimSpace(partes[0]), 64)
		fim, err2 := strconv.ParseFloat(strings.TrimSpace(partes[1]), 64)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("ajuste %q: tempos ilegíveis", chave)
		}
		out = append(out, ajusteRecebido{Indice: idx, Start: ini, End: fim})
	}
	return out, nil
}

// validarIndices confere que todos os índices caem em [0, n) e remove duplicatas,
// preservando a ordem. Lista vazia é válida (o operador reprovou tudo).
func validarIndices(indices []int, n int) ([]int, bool) {
	visto := make(map[int]bool, len(indices))
	var out []int
	for _, i := range indices {
		if i < 0 || i >= n {
			return nil, false
		}
		if !visto[i] {
			visto[i] = true
			out = append(out, i)
		}
	}
	return out, true
}

// faseLeve é a máquina de estados da fase leve: baixando-legenda → selecionando →
// validando → aguardando-aprovacao (ou erro, com mensagem clara). Roda em goroutine.
func (s *Servidor) faseLeve(reg *registro) {
	ctx := context.Background()
	idPedidoLeve := s.copiaPedido(reg).ID

	s.setStatus(reg, pipeline.EstadoBaixandoLegenda)
	// Cópia pelo mesmo motivo da fase pesada: o Baixador escreve no Pedido, e o registro é
	// compartilhado com os handlers. Aqui a cópia traz de volta o título (do .info.json).
	copia := s.copiaPedido(reg)
	if err := etapaComPrazo(ctx, "o download da legenda", s.prazos.Legenda, func(ctx context.Context) error {
		return s.baixador.BaixarLegenda(ctx, copia)
	}); err != nil {
		s.setErro(reg, mensagemErroDownload(err))
		return
	}
	s.aplicarTitulo(reg, copia)
	s.metrica(reg, func(m *Metricas) {
		m.BaixarLegendaMs = m.marcar(s.agora())
		m.Titulo = copia.Titulo
	})

	s.setStatus(reg, pipeline.EstadoSelecionando)
	transc := filepath.Join(s.baseDir, idPedidoLeve, "transcricao.txt")
	s.metrica(reg, func(m *Metricas) { m.TokensTranscricao = tokensAprox(transc) })
	var cands []validacao.Candidato
	err := etapaComPrazo(ctx, "a seleção dos trechos", s.prazos.Selecao, func(ctx context.Context) error {
		var e error
		cands, e = s.selecionador.Selecionar(ctx, transc)
		return e
	})
	if err != nil {
		s.setErro(reg, comPrefixo("falha na seleção: ", err))
		return
	}
	s.metrica(reg, func(m *Metricas) {
		m.SelecionarMs = m.marcar(s.agora())
		m.NumCandidatos = len(cands)
	})

	s.setStatus(reg, pipeline.EstadoValidando)
	if err := salvarCandidatos(s.baseDir, reg.ped.ID, cands); err != nil {
		s.setErro(reg, "falha ao gravar os candidatos: "+err.Error())
		return
	}

	// Texto REALMENTE falado em cada trecho (o artefato de revisão): reconstruído da
	// transcrição via harness.Frasear, o mesmo do cmd/auditar. Best-effort.
	textos := textosFalados(transc, cands)

	// Registra a rodada em disco (avaliação de variância) antes de expor ao operador.
	// Falha de log não interrompe a seleção — é auxiliar.
	s.registrarRodada(reg.ped, cands)

	s.mu.Lock()
	reg.cands = cands
	reg.textos = textos
	reg.metricas.ValidarMs = reg.metricas.marcar(s.agora())
	reg.ped.Status = pipeline.EstadoAguardandoAprovacao
	s.mu.Unlock()
}

// metrica aplica uma escrita nas métricas do pedido SOB LOCK. As fases rodam em goroutine
// e o mesmo registro é tocado pelos handlers HTTP; sem o lock, o -race acusa (com razão).
// Nil-safe: depois de finalizarPedido as métricas são zeradas e a escrita vira no-op.
func (s *Servidor) metrica(reg *registro, fn func(m *Metricas)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if reg.metricas != nil {
		fn(reg.metricas)
	}
}

// copiaPedido devolve uma cópia do Pedido para entregar a dependências que escrevem nele
// (o Baixador preenche Status/Erro/Titulo — contrato do CLI). O registro é compartilhado
// com os handlers HTTP, então deixar uma goroutine escrever ali direto é corrida real: o
// handleStatus lê Status sob lock enquanto o download escreveria sem. O servidor é o dono
// do status; da cópia aproveitamos só o que o baixador legitimamente descobre (o título).
func (s *Servidor) copiaPedido(reg *registro) *pipeline.Pedido {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := *reg.ped
	return &c
}

// aplicarTitulo copia de volta, sob lock, o título que o baixador descobriu no .info.json.
func (s *Servidor) aplicarTitulo(reg *registro, copia *pipeline.Pedido) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if copia.Titulo != "" {
		reg.ped.Titulo = copia.Titulo
	}
}

func (s *Servidor) setStatus(reg *registro, e pipeline.Estado) {
	s.mu.Lock()
	reg.ped.Status = e
	s.mu.Unlock()
}

// setErro põe o pedido em erro E fecha a auditoria de tempo. Um pedido que falha também
// consumiu tempo — e esse tempo é parte real da percepção do operador, que vai refazer.
// Gravar só o sucesso deixaria a média otimista (spec-06/instrumentação).
func (s *Servidor) setErro(reg *registro, msg string) {
	s.mu.Lock()
	reg.ped.Status = pipeline.EstadoErro
	reg.ped.Erro = msg
	id := reg.ped.ID
	s.mu.Unlock()
	s.finalizarPedido(reg, msg)
	// O pedido que falhou não tem Short a regerar e pode ter deixado lixo (mp4 parcial,
	// .part, .ytdl). Limpar na hora: falha costuma ocorrer com o disco apertado, e
	// acumular resíduo de falhas realimentaria o problema.
	s.limparResiduoDeErro(id)
}

// finalizarPedido fecha a auditoria de UM pedido: registra os retries, loga o resumo e
// grava a linha no CSV. Idempotente — chamada mais de uma vez (erro após erro), grava só
// na primeira: `metricas` é zerado depois de gravar.
func (s *Servidor) finalizarPedido(reg *registro, erro string) {
	s.mu.Lock()
	met := reg.metricas
	reg.metricas = nil // marca como já finalizado
	s.mu.Unlock()
	if met == nil {
		return
	}
	met.Erro = erro
	met.Completou = erro == ""
	met.FecharRetries()
	s.logTempos(met.Resumo())
	s.gravarTempos(met)
}

// lerEntrada aceita tanto JSON (Content-Type: application/json) quanto formulário.
func lerEntrada(r *http.Request) (entrada, error) {
	var ent entrada
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&ent); err != nil {
			return ent, err
		}
		return ent, nil
	}
	if err := r.ParseForm(); err != nil {
		return ent, err
	}
	ent.YouTubeURL = strings.TrimSpace(r.FormValue("youtube_url"))
	ent.Inicio = strings.TrimSpace(r.FormValue("inicio"))
	ent.Fim = strings.TrimSpace(r.FormValue("fim"))
	return ent, nil
}

// validarEntrada checa URL do YouTube e tempos (HH:MM:SS com fim > início). Devolve a
// mensagem de erro (vazia se ok) e um booleano.
func validarEntrada(e entrada) (string, bool) {
	if !urlYouTube(e.YouTubeURL) {
		return "informe um link válido do YouTube (youtube.com ou youtu.be)", false
	}
	i, oki := transcricao.HmsToMs(e.Inicio)
	f, okf := transcricao.HmsToMs(e.Fim)
	if !oki || !okf {
		return "use HH:MM:SS nos tempos de início e fim", false
	}
	if f <= i {
		return "o fim da pregação deve ser maior que o início", false
	}
	return "", true
}

// urlYouTube confere se a URL parece um link do YouTube (host youtube.com/youtu.be).
func urlYouTube(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")
	switch {
	case host == "youtu.be", host == "youtube.com", host == "m.youtube.com",
		strings.HasSuffix(host, ".youtube.com"):
		return true
	}
	return false
}

// querJSON diz se o cliente prefere JSON (Accept: application/json). O navegador
// (HTMX) manda text/html; clientes de API pedem JSON.
func querJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}

// mensagemErroDownload traduz os erros do download em mensagens claras para o operador.
func mensagemErroDownload(err error) string {
	switch {
	case errors.Is(err, ErrPrazoEstourado):
		return err.Error() // já nomeia a etapa e o prazo
	case errors.Is(err, download.ErrAntiBot):
		return "o YouTube pediu verificação anti-robô (muitas requisições); aguarde alguns minutos e tente novamente"
	case errors.Is(err, download.ErrSemLegenda):
		return "este vídeo não tem legenda automática em português — sem legenda não dá para selecionar (não transcrevemos localmente)"
	case errors.Is(err, download.ErrVideoIndisponivel):
		return "vídeo indisponível (privado, removido ou restrito)"
	case errors.Is(err, download.ErrTempoInvalido):
		return "tempos inválidos: use HH:MM:SS com fim maior que início"
	default:
		return "falha ao baixar a legenda: " + err.Error()
	}
}
