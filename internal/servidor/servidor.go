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
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"srtclean/internal/download"
	"srtclean/internal/pipeline"
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

// BaixadorVideo baixa APENAS a janela [inicio, fim] do vídeo (fase pesada, spec-05 parte 3).
type BaixadorVideo interface {
	BaixarVideoJanela(ctx context.Context, ped *pipeline.Pedido, inicio, fim string) error
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
	shorts    []string // nomes dos arquivos finais gerados (fase pesada), para download
}

// Servidor guarda as dependências e o registro em memória dos pedidos.
type Servidor struct {
	baixador       BaixadorLegenda
	selecionador   Selecionador
	baixadorVideo  BaixadorVideo
	renderizador   RenderizadorVideo
	baseDir        string
	outDir         string
	logRodadasPath string
	assetsDir      string
	agora          func() time.Time
	gerarID        func() string
	mux            *http.ServeMux

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
	// AssetsDir é a pasta servida em /assets/ (fonte da identidade, logo). Vazio = "assets".
	AssetsDir string
	Agora     func() time.Time // injetável para testes
	GerarID   func() string    // injetável para testes
}

// Novo cria o servidor e registra as rotas.
func Novo(o Opcoes) *Servidor {
	s := &Servidor{
		baixador:       o.Baixador,
		selecionador:   o.Selecionador,
		baixadorVideo:  o.BaixadorVideo,
		renderizador:   o.Renderizador,
		baseDir:        o.BaseDir,
		outDir:         o.OutDir,
		logRodadasPath: o.LogRodadasPath,
		assetsDir:      o.AssetsDir,
		agora:          o.Agora,
		gerarID:        o.GerarID,
		pedidos:        make(map[string]*registro),
		mux:            http.NewServeMux(),
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
	reg := &registro{ped: ped}
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

	indices, err := lerAprovados(r)
	if err != nil {
		s.responderErroAprovar(w, r, http.StatusBadRequest, "não entendi a lista de aprovados")
		return
	}
	limpos, ok := validarIndices(indices, nCands)
	if !ok {
		s.responderErroAprovar(w, r, http.StatusBadRequest, "índices de aprovação fora do intervalo dos candidatos")
		return
	}

	s.mu.Lock()
	reg.aprovados = limpos
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
// Alinhamento de tempo (cuidado crítico da spec): baixa APENAS a janela contígua que cobre
// todos os trechos aprovados — [menor start, maior end] — via --download-sections. O
// arquivo resultante começa em t=0 no MENOR start (origemMs); o render recebe essa MESMA
// origem (RenderizarComOrigem), então cada corte é (start - origemMs), casando exatamente.
func (s *Servidor) faseHeavy(reg *registro) {
	ctx := context.Background()

	s.mu.Lock()
	aprovados := candidatosAprovados(reg)
	s.mu.Unlock()
	if len(aprovados) == 0 {
		s.setErro(reg, "nenhum trecho aprovado para gerar")
		return
	}

	// Janela de download = [menor start, maior end] dos aprovados; origem = menor start
	// (piso ao segundo, o que o yt-dlp recebe). Ver janelaDownload.
	iniHms, fimHms, origemMs := janelaDownload(aprovados)

	s.setStatus(reg, pipeline.EstadoBaixandoVideo)
	if err := s.baixadorVideo.BaixarVideoJanela(ctx, reg.ped, iniHms, fimHms); err != nil {
		s.setErro(reg, mensagemErroDownload(err))
		return
	}

	s.setStatus(reg, pipeline.EstadoRenderizando)
	paths, err := s.renderizador.RenderizarComOrigem(ctx, reg.ped, aprovados, origemMs)
	if err != nil {
		s.setErro(reg, "falha ao renderizar os Shorts: "+err.Error())
		return
	}

	nomes := make([]string, len(paths))
	for i, p := range paths {
		nomes[i] = filepath.Base(p)
	}
	s.mu.Lock()
	reg.shorts = nomes
	reg.ped.Status = pipeline.EstadoConcluido
	s.mu.Unlock()
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
func lerAprovados(r *http.Request) ([]int, error) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		defer r.Body.Close()
		var corpo struct {
			Aprovados []int `json:"aprovados"`
		}
		if err := json.NewDecoder(r.Body).Decode(&corpo); err != nil {
			return nil, err
		}
		return corpo.Aprovados, nil
	}
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	var out []int
	for _, s := range r.Form["aprovados"] {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return nil, err
		}
		out = append(out, n)
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

	s.setStatus(reg, pipeline.EstadoBaixandoLegenda)
	if err := s.baixador.BaixarLegenda(ctx, reg.ped); err != nil {
		s.setErro(reg, mensagemErroDownload(err))
		return
	}

	s.setStatus(reg, pipeline.EstadoSelecionando)
	transc := filepath.Join(s.baseDir, reg.ped.ID, "transcricao.txt")
	cands, err := s.selecionador.Selecionar(ctx, transc)
	if err != nil {
		s.setErro(reg, "falha na seleção: "+err.Error())
		return
	}

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
	reg.ped.Status = pipeline.EstadoAguardandoAprovacao
	s.mu.Unlock()
}

func (s *Servidor) setStatus(reg *registro, e pipeline.Estado) {
	s.mu.Lock()
	reg.ped.Status = e
	s.mu.Unlock()
}

func (s *Servidor) setErro(reg *registro, msg string) {
	s.mu.Lock()
	reg.ped.Status = pipeline.EstadoErro
	reg.ped.Erro = msg
	s.mu.Unlock()
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
